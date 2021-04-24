package main

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/sgielen/rufs/discovery/orchestrate"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"golang.org/x/sync/errgroup"
)

var (
	activeOrchestrationMtx sync.Mutex
	activeOrchestration    map[int64]*orchestration
)

type orchestration struct {
	discovery      *discovery
	activeDownload *pb.ConnectResponse_ActiveDownload

	mtx         sync.Mutex
	schedCond   *sync.Cond
	connections map[string]*orchestrationClient
	scheduler   *orchestrate.Orchestrator
}

type orchestrationClient struct {
	peer           string
	o              *orchestration
	stream         pb.DiscoveryService_OrchestrateServer
	cond           *sync.Cond
	updatePeerList bool
	uploadCommands []*pb.OrchestrateResponse_UploadCommand
}

func (d *discovery) Orchestrate(stream pb.DiscoveryService_OrchestrateServer) error {
	peer, _, err := security.PeerFromContext(stream.Context())
	if err != nil {
		return err
	}
	msg, err := stream.Recv()
	if err != nil {
		return err
	}
	if msg.GetStartOrchestration() == nil {
		return errors.New("expected start_orchestration to be set")
	}
	activeOrchestrationMtx.Lock()
	o, ok := activeOrchestration[msg.GetStartOrchestration().GetDownloadId()]
	if !ok {
		for _, ao := range activeOrchestration {
			if msg.GetStartOrchestration().GetHash() == ao.activeDownload.GetHash() && msg.GetStartOrchestration().GetHash() != "" {
				activeOrchestrationMtx.Unlock()
				return fmt.Errorf("attempt to start new orchestration for active download of hash %q", msg.GetStartOrchestration().GetHash())
			}
		}
		o := &orchestration{
			discovery: d,
			activeDownload: &pb.ConnectResponse_ActiveDownload{
				DownloadId: rand.Int63(),
				Hash:       msg.GetStartOrchestration().GetHash(),
				Filenames:  []string{msg.GetStartOrchestration().GetFilename()},
			},
			scheduler: orchestrate.New(),
		}
		o.schedCond = sync.NewCond(&o.mtx)
		if msg.GetStartOrchestration().GetDownloadId() != 0 {
			// Allow resuming after discovery server restarts.
			o.activeDownload.DownloadId = msg.GetStartOrchestration().GetDownloadId()
		}
		activeOrchestration[o.activeDownload.GetDownloadId()] = o
		d.broadcastNewActiveDownloads()
	}
	activeOrchestrationMtx.Unlock()
	if err := stream.Send(&pb.OrchestrateResponse{
		Msg: &pb.OrchestrateResponse_Welcome_{
			Welcome: &pb.OrchestrateResponse_Welcome{
				DownloadId: o.activeDownload.GetDownloadId(),
			},
		},
	}); err != nil {
		return err
	}
	c := &orchestrationClient{
		o:      o,
		stream: stream,
	}
	c.cond = sync.NewCond(&o.mtx)
	o.mtx.Lock()
	o.connections[peer] = c
	for _, conn := range o.connections {
		conn.updatePeerList = true
		conn.cond.Broadcast()
	}
	o.mtx.Unlock()
	var g errgroup.Group
	g.Go(c.reader)
	g.Go(c.writer)
	err = g.Wait()
	o.mtx.Lock()
	if o.connections[peer] == c {
		delete(o.connections, peer)
	}
	o.mtx.Unlock()
	return err
}

func (c *orchestrationClient) reader() error {
	for {
		msg, err := c.stream.Recv()
		if err != nil {
			return err
		}
		c.o.mtx.Lock()
		if c.o.connections[c.peer] != c {
			c.o.mtx.Unlock()
			return errors.New("second Orchestrate call cancelled this one")
		}
		if msg.GetUpdateByteRanges() != nil {
			c.o.scheduler.UpdateByteRanges(c.peer, msg.GetUpdateByteRanges())
		}
		if msg.GetConnectedPeers() != nil {
			c.o.scheduler.SetConnectedPeers(c.peer, msg.GetConnectedPeers())
		}
		if msg.GetUploadFailed() != nil {
			c.o.scheduler.UploadFailed(c.peer, msg.GetUploadFailed())
		}
		if msg.GetSetHash() != nil {
			if c.o.activeDownload.Hash != "" {
				c.o.mtx.Unlock()
				return errors.New("hash is already known for this orchestration")
			}
			// Clone before changing as we might be stream.Send()ing the old proto concurrently.
			nad := proto.Clone(c.o.activeDownload).(*pb.ConnectResponse_ActiveDownload)
			nad.Hash = msg.GetSetHash().GetHash()
			c.o.activeDownload = nad
			c.o.discovery.broadcastNewActiveDownloads()
		}
		c.o.mtx.Unlock()
	}
}

func (c *orchestrationClient) writer() error {
	c.o.mtx.Lock()
	defer c.o.mtx.Unlock()
	for {
		for c.updatePeerList || len(c.uploadCommands) > 0 {
			msg := &pb.OrchestrateResponse{}
			if len(c.uploadCommands) > 0 {
				msg.Msg = &pb.OrchestrateResponse_UploadCommand_{
					UploadCommand: c.uploadCommands[0],
				}
				c.uploadCommands = c.uploadCommands[1:]
			} else {
				peers := make([]string, 0, len(c.o.connections))
				for p := range c.o.connections {
					peers = append(peers, p)
				}
				msg.Msg = &pb.OrchestrateResponse_PeerList_{
					PeerList: &pb.OrchestrateResponse_PeerList{
						Peers: peers,
					},
				}
			}
			if err := c.stream.Send(msg); err != nil {
				return err
			}
		}
		c.cond.Wait()
		if c.o.connections[c.peer] != c {
			return errors.New("second Orchestrate call cancelled this one")
		}
	}
}

func (o *orchestration) schedulerThread() {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	for {
		o.schedCond.Wait()
		transfers := o.scheduler.ComputeNewTransfers()
		for p, uploads := range transfers {
			c := o.connections[p]
			c.uploadCommands = append(c.uploadCommands, uploads...)
			c.cond.Broadcast()
		}
	}
}
