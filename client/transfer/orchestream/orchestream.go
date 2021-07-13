// Package orchestream calls the DiscoveryService.Orchestrate RPC and provides a nice API.
package orchestream

import (
	"context"
	"io"
	"log"
	"sync"

	"github.com/cenkalti/backoff/v4"
	"github.com/sgielen/rufs/client/connectivity"
	pb "github.com/sgielen/rufs/proto"
	"google.golang.org/protobuf/proto"
)

type StreamClient interface {
	Welcome(downloadId int64)
	SetPeers(ctx context.Context, peers []string)
	Upload(ctx context.Context, peer string, byteRange *pb.Range)
	OrchestrationClosed()
}

func New(ctx context.Context, circle string, start *pb.OrchestrateRequest_StartOrchestrationRequest, callbacks StreamClient) (*Stream, error) {
	ctx, cancel := context.WithCancel(ctx)
	stream, err := connectivity.DiscoveryClient(circle).Orchestrate(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := stream.Send(&pb.OrchestrateRequest{
		Msg: &pb.OrchestrateRequest_StartOrchestration{
			StartOrchestration: start,
		},
	}); err != nil {
		stream.CloseSend()
		cancel()
		return nil, err
	}
	msg, err := stream.Recv()
	if err != nil {
		stream.CloseSend()
		cancel()
		return nil, err
	}
	s := &Stream{
		DownloadId:     msg.GetWelcome().GetDownloadId(),
		circle:         circle,
		stream:         stream,
		callbacks:      callbacks,
		cancel:         cancel,
		haveHandles:    false,
		setHaveHandles: true,
		reconnectStart: proto.Clone(start).(*pb.OrchestrateRequest_StartOrchestrationRequest),
	}
	callbacks.Welcome(s.DownloadId)
	s.reconnectStart.DownloadId = s.DownloadId
	s.cond = sync.NewCond(&s.mtx)
	go s.reader(ctx)
	go s.writer(ctx)
	return s, nil
}

type Stream struct {
	DownloadId int64
	circle     string
	callbacks  StreamClient
	cancel     func()

	mtx                  sync.Mutex
	cond                 *sync.Cond
	stream               pb.DiscoveryService_OrchestrateClient
	reconnecting         bool
	setHash              string
	updateByteRanges     bool
	ranges               *pb.OrchestrateRequest_UpdateByteRanges
	updateConnectedPeers bool
	connectedPeers       []string
	failedUploads        []string
	haveHandles          bool
	setHaveHandles       bool
	reconnectStart       *pb.OrchestrateRequest_StartOrchestrationRequest
}

func (s *Stream) reader(ctx context.Context) {
	for {
		msg, err := s.stream.Recv()
		if err == io.EOF {
			s.callbacks.OrchestrationClosed()
			s.Close()
			return
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("Lost connection to orchestrator. Reconnecting...")
			if err2 := backoff.Retry(func() error {
				return s.reconnect(ctx)
			}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx)); err2 != nil {
				log.Printf("Failed to reconnect to orchestrator: %v", err2)
			}
			continue
		}
		if msg.GetPeerList() != nil {
			s.callbacks.SetPeers(ctx, msg.GetPeerList().GetPeers())
		}
		if msg.GetUploadCommand() != nil {
			s.callbacks.Upload(ctx, msg.GetUploadCommand().GetPeer(), msg.GetUploadCommand().GetRange())
		}
	}
}

func (s *Stream) writer(ctx context.Context) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for {
		for !s.reconnecting && (s.updateByteRanges || s.updateConnectedPeers || s.setHash != "" || len(s.failedUploads) > 0 || s.setHaveHandles) {
			msg := &pb.OrchestrateRequest{}
			if s.updateByteRanges {
				msg.Msg = &pb.OrchestrateRequest_UpdateByteRanges_{
					UpdateByteRanges: s.ranges,
				}
				s.updateByteRanges = false
			} else if s.updateConnectedPeers {
				msg.Msg = &pb.OrchestrateRequest_ConnectedPeers_{
					ConnectedPeers: &pb.OrchestrateRequest_ConnectedPeers{
						Peers: s.connectedPeers,
					},
				}
				s.updateConnectedPeers = false
			} else if s.setHash != "" {
				msg.Msg = &pb.OrchestrateRequest_SetHash_{
					SetHash: &pb.OrchestrateRequest_SetHash{
						Hash: s.setHash,
					},
				}
				s.setHash = ""
			} else if s.setHaveHandles {
				msg.Msg = &pb.OrchestrateRequest_HaveOpenHandles_{
					HaveOpenHandles: &pb.OrchestrateRequest_HaveOpenHandles{
						HaveOpenHandles: s.haveHandles,
					},
				}
				s.setHaveHandles = false
			} else {
				msg.Msg = &pb.OrchestrateRequest_UploadFailed_{
					UploadFailed: &pb.OrchestrateRequest_UploadFailed{
						TargetPeers: s.failedUploads,
					},
				}
				s.failedUploads = nil
			}
			stream := s.stream
			s.mtx.Unlock()
			err := stream.Send(msg)
			s.mtx.Lock()
			if err != nil {
				if err == io.EOF {
					// ignore, reader will find it soon
					return
				} else {
					s.reconnecting = true
				}
			}
		}
		s.cond.Wait()
		if ctx.Err() != nil {
			return
		}
	}
}

func (s *Stream) reconnect(ctx context.Context) error {
	s.mtx.Lock()
	// Prevent the writer thread from touching the stream.
	s.reconnecting = true
	reconnectStart := proto.Clone(s.reconnectStart).(*pb.OrchestrateRequest_StartOrchestrationRequest)
	s.mtx.Unlock()

	stream, err := connectivity.DiscoveryClient(s.circle).Orchestrate(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.OrchestrateRequest{
		Msg: &pb.OrchestrateRequest_StartOrchestration{
			StartOrchestration: reconnectStart,
		},
	}); err != nil {
		stream.CloseSend()
		return err
	}
	if _, err := stream.Recv(); err != nil {
		stream.CloseSend()
		return err
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.stream = stream
	s.updateByteRanges = true
	s.updateConnectedPeers = true
	s.failedUploads = nil
	s.setHaveHandles = true
	s.reconnecting = false
	s.cond.Broadcast()
	return nil
}

func (s *Stream) SetByteRanges(ranges *pb.OrchestrateRequest_UpdateByteRanges) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.ranges = ranges
	s.updateByteRanges = true
	s.cond.Broadcast()
}

func (s *Stream) SetConnectedPeers(peers []string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.connectedPeers = peers
	s.updateConnectedPeers = true
	s.cond.Broadcast()
}

func (s *Stream) UploadFailed(peer string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.failedUploads = append(s.failedUploads, peer)
	s.cond.Broadcast()
}

func (s *Stream) SetHash(hash string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.setHash = hash
	s.reconnectStart.Hash = hash
	s.cond.Broadcast()
}

func (s *Stream) SetHaveHandles(h bool) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.haveHandles != h {
		s.haveHandles = h
		s.setHaveHandles = true
		s.cond.Broadcast()
	}
}

func (s *Stream) Close() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.cancel()
	s.cond.Broadcast()
	return nil
}
