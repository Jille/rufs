// Package orchestream calls the DiscoveryService.Orchestrate RPC and provides a nice API.
package orchestream

import (
	"context"
	"sync"

	"github.com/sgielen/rufs/client/connectivity"
	pb "github.com/sgielen/rufs/proto"
)

type StreamClient interface {
	SetPeers(ctx context.Context, peers []string)
	Upload(ctx context.Context, peer string, byteRange *pb.Range)
}

func New(ctx context.Context, circle string, start *pb.OrchestrateRequest_StartOrchestrationRequest, callbacks StreamClient) (*Stream, error) {
	ctx, cancel := context.WithCancel(ctx)
	stream, err := connectivity.DiscoveryClient(circle).Orchestrate(ctx)
	if err != nil {
		return nil, err
	}
	if err := stream.Send(&pb.OrchestrateRequest{
		Msg: &pb.OrchestrateRequest_StartOrchestration{
			StartOrchestration: start,
		},
	}); err != nil {
		stream.CloseSend()
		return nil, err
	}
	msg, err := stream.Recv()
	if err != nil {
		stream.CloseSend()
		return nil, err
	}
	s := &Stream{
		DownloadId: msg.GetWelcome().GetDownloadId(),
		stream:     stream,
		callbacks:  callbacks,
		cancel:     cancel,
	}
	s.cond = sync.NewCond(&s.mtx)
	go s.reader(ctx)
	go s.writer(ctx)
	return s, nil
}

type Stream struct {
	DownloadId int64
	stream     pb.DiscoveryService_OrchestrateClient
	callbacks  StreamClient
	cancel     func()

	mtx                  sync.Mutex
	cond                 *sync.Cond
	updateByteRanges     bool
	ranges               *pb.OrchestrateRequest_UpdateByteRanges
	updateConnectedPeers bool
	connectedPeers       []string
	failedUploads        []string
}

func (s *Stream) reader(ctx context.Context) {
	for {
		msg, err := s.stream.Recv()
		if err != nil {
			// TODO: reconnect
			panic(err)
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
		msg := &pb.OrchestrateRequest{}
		for s.updateByteRanges || s.updateConnectedPeers || len(s.failedUploads) > 0 {
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
			} else {
				msg.Msg = &pb.OrchestrateRequest_UploadFailed_{
					UploadFailed: &pb.OrchestrateRequest_UploadFailed{
						TargetPeers: s.failedUploads,
					},
				}
				s.failedUploads = nil
			}
			s.mtx.Unlock()
			if err := s.stream.Send(msg); err != nil {
				s.mtx.Lock()
				// TODO: reconnect?
				panic(err)
			}
			s.mtx.Lock()
		}
		s.cond.Wait()
	}
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

func (s *Stream) Close() error {
	s.cancel()
	return nil
}
