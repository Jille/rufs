package main

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/Jille/rufs/discovery/metrics"
	"github.com/Jille/rufs/discovery/orchestrate"
	pb "github.com/Jille/rufs/proto"
	"github.com/Jille/rufs/security"
	"github.com/golang/protobuf/proto"
	"github.com/ory/go-convenience/stringslice"
)

var (
	activeOrchestrationMtx sync.Mutex
	activeOrchestration    = map[int64]*orchestration{}

	schedulerBusynessMetric = metrics.NewBusynessMetric("orchestrate_scheduler", []string{"orchestration"})
)

type orchestration struct {
	discovery      *discovery
	activeDownload *pb.ConnectResponse_ActiveDownload
	handlesChan    chan struct{}

	mtx               sync.Mutex
	closing           bool
	schedCond         *sync.Cond
	connections       map[string]*orchestrationClient
	scheduler         *orchestrate.Orchestrator
	schedulerBusyness *metrics.BusynessSubMetric
}

type orchestrationClient struct {
	peer           string
	o              *orchestration
	initiator      bool
	stream         pb.DiscoveryService_OrchestrateServer
	cond           *sync.Cond
	updatePeerList bool
	uploadCommands []*pb.OrchestrateResponse_UploadCommand
	disconnecting  bool
	hasOpenHandles bool
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
	log.Printf("Orchestrate [%s] StartOrchestration: %s", peer, msg.GetStartOrchestration())
	activeOrchestrationMtx.Lock()
	o, ok := activeOrchestration[msg.GetStartOrchestration().GetDownloadId()]
	if !ok {
		for _, ao := range activeOrchestration {
			if msg.GetStartOrchestration().GetHash() == ao.activeDownload.GetHash() && msg.GetStartOrchestration().GetHash() != "" {
				activeOrchestrationMtx.Unlock()
				return fmt.Errorf("attempt to start new orchestration for active download of hash %q", msg.GetStartOrchestration().GetHash())
			}
		}
		o = &orchestration{
			discovery: d,
			activeDownload: &pb.ConnectResponse_ActiveDownload{
				DownloadId: rand.Int63(),
				Hash:       msg.GetStartOrchestration().GetHash(),
			},
			connections: map[string]*orchestrationClient{},
			scheduler:   orchestrate.New(),
			handlesChan: make(chan struct{}, 10),
		}
		o.schedCond = sync.NewCond(&o.mtx)
		if msg.GetStartOrchestration().GetFilename() != "" {
			o.activeDownload.Filenames = append(o.activeDownload.Filenames, msg.GetStartOrchestration().GetFilename())
		}
		if msg.GetStartOrchestration().GetDownloadId() != 0 {
			// Allow resuming after discovery server restarts.
			o.activeDownload.DownloadId = msg.GetStartOrchestration().GetDownloadId()
		}
		o.schedulerBusyness = schedulerBusynessMetric.Instance([]string{fmt.Sprint(o.activeDownload.DownloadId)})
		activeOrchestration[o.activeDownload.GetDownloadId()] = o
		d.broadcastNewActiveDownloads()
		go o.schedulerThread()
		go o.cleanupThread()
	} else if msg.GetStartOrchestration().GetFilename() != "" && !stringslice.Has(o.activeDownload.Filenames, msg.GetStartOrchestration().GetFilename()) {
		nad := proto.Clone(o.activeDownload).(*pb.ConnectResponse_ActiveDownload)
		nad.Filenames = append(nad.Filenames, msg.GetStartOrchestration().GetFilename())
		o.activeDownload = nad
		d.broadcastNewActiveDownloads()
	}
	activeOrchestrationMtx.Unlock()
	log.Printf("Orchestrate{%d} [%s] Sending: welcome:{download_id: %d}", o.activeDownload.GetDownloadId(), peer, o.activeDownload.GetDownloadId())
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
		initiator: msg.GetStartOrchestration().GetDownloadId() == 0,
		peer:      peer,
		o:         o,
		stream:    stream,
	}
	c.cond = sync.NewCond(&o.mtx)
	o.mtx.Lock()
	o.connections[peer] = c
	c.o.handlesChan <- struct{}{}
	for _, conn := range o.connections {
		conn.updatePeerList = true
		conn.cond.Broadcast()
	}
	o.mtx.Unlock()
	errCh := make(chan error, 2)
	go func() {
		errCh <- c.reader()
	}()
	go func() {
		errCh <- c.writer()
	}()
	err = <-errCh
	log.Printf("Orchestrate{%d} [%s] reader or writer thread died: %v", c.o.activeDownload.GetDownloadId(), c.peer, err)
	o.mtx.Lock()
	c.disconnecting = true
	o.scheduler.Disappeered(peer)
	c.cond.Broadcast()
	if o.connections[peer] == c {
		delete(o.connections, peer)
		c.o.handlesChan <- struct{}{}
	}
	o.mtx.Unlock()
	return err
}

func (c *orchestrationClient) reader() error {
	for {
		msg, err := c.stream.Recv()
		if err != nil {
			log.Printf("Orchestrate{%d} [%s] reader died: %v", c.o.activeDownload.GetDownloadId(), c.peer, err)
			return err
		}
		c.o.mtx.Lock()
		if c.o.connections[c.peer] != c {
			c.o.mtx.Unlock()
			return errors.New("second Orchestrate call cancelled this one")
		}
		if msg.GetUpdateByteRanges() != nil {
			log.Printf("Orchestrate{%d} [%s] UpdateByteRanges: %s", c.o.activeDownload.GetDownloadId(), c.peer, msg.GetUpdateByteRanges())
			c.o.scheduler.UpdateByteRanges(c.peer, msg.GetUpdateByteRanges())
			c.o.schedCond.Broadcast()
		}
		if msg.GetConnectedPeers() != nil {
			log.Printf("Orchestrate{%d} [%s] SetConnectedPeers: %s", c.o.activeDownload.GetDownloadId(), c.peer, msg.GetConnectedPeers())
			c.o.scheduler.SetConnectedPeers(c.peer, msg.GetConnectedPeers())
			c.o.schedCond.Broadcast()
		}
		if msg.GetUploadFailed() != nil {
			log.Printf("Orchestrate{%d} [%s] UploadFailed: %s", c.o.activeDownload.GetDownloadId(), c.peer, msg.GetUploadFailed())
			c.o.scheduler.UploadFailed(c.peer, msg.GetUploadFailed())
			c.o.schedCond.Broadcast()
		}
		if msg.GetSetHash() != nil {
			log.Printf("Orchestrate{%d} [%s] SetHash: %s", c.o.activeDownload.GetDownloadId(), c.peer, msg.GetSetHash())
			if !c.initiator {
				c.o.mtx.Unlock()
				return errors.New("you are not the initiator of this orchestration so can't set the hash")
			}
			if c.o.activeDownload.Hash != "" && c.o.activeDownload.Hash != msg.GetSetHash().GetHash() {
				c.o.mtx.Unlock()
				return fmt.Errorf("refusing attempt to change hash from %q to %q", c.o.activeDownload.Hash, msg.GetSetHash().GetHash())
			}
			// Clone before changing as we might be stream.Send()ing the old proto concurrently.
			nad := proto.Clone(c.o.activeDownload).(*pb.ConnectResponse_ActiveDownload)
			nad.Hash = msg.GetSetHash().GetHash()
			c.o.activeDownload = nad
			c.o.discovery.broadcastNewActiveDownloads()
		}
		if msg.GetHaveOpenHandles() != nil {
			log.Printf("Orchestrate{%d} [%s] HaveOpenHandles: %t", c.o.activeDownload.GetDownloadId(), c.peer, msg.GetHaveOpenHandles().GetHaveOpenHandles())
			c.hasOpenHandles = msg.GetHaveOpenHandles().GetHaveOpenHandles()
			c.o.handlesChan <- struct{}{}
		}
		c.o.mtx.Unlock()
	}
}

func (c *orchestrationClient) writer() error {
	c.o.mtx.Lock()
	defer c.o.mtx.Unlock()
	for {
		for c.updatePeerList || len(c.uploadCommands) > 0 {
			if c.disconnecting {
				return nil
			}
			msg := &pb.OrchestrateResponse{}
			if len(c.uploadCommands) > 0 {
				msg.Msg = &pb.OrchestrateResponse_UploadCommand_{
					UploadCommand: c.uploadCommands[0],
				}
				c.uploadCommands = c.uploadCommands[1:]
			} else {
				peers := make([]string, 0, len(c.o.connections))
				for p := range c.o.connections {
					if p == c.peer {
						continue
					}
					peers = append(peers, p)
				}
				msg.Msg = &pb.OrchestrateResponse_PeerList_{
					PeerList: &pb.OrchestrateResponse_PeerList{
						Peers: peers,
					},
				}
				c.updatePeerList = false
			}
			c.o.mtx.Unlock()
			log.Printf("Orchestrate{%d} [%s] Sending: %s", c.o.activeDownload.GetDownloadId(), c.peer, msg)
			if err := c.stream.Send(msg); err != nil {
				c.o.mtx.Lock()
				return err
			}
			c.o.mtx.Lock()
		}
		if c.disconnecting {
			return nil
		}
		c.cond.Wait()
		if c.disconnecting {
			return nil
		}
		if c.o.connections[c.peer] != c {
			return errors.New("second Orchestrate call cancelled this one")
		}
	}
}

func (o *orchestration) schedulerThread() {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	for !o.closing {
		o.schedulerBusyness.Idle()
		o.schedCond.Wait()
		o.schedulerBusyness.Busy()
		if o.closing {
			break
		}
		transfers := o.scheduler.ComputeNewTransfers()
		for p, uploads := range transfers {
			if c, ok := o.connections[p]; ok {
				c.uploadCommands = append(c.uploadCommands, uploads...)
				c.cond.Broadcast()
			} else {
				log.Printf("Scheduler wanted %q to upload, but they're disconnected", p)
				var targets []string
				for _, t := range uploads {
					targets = append(targets, t.GetPeer())
				}
				targets = stringslice.Unique(targets)
				o.scheduler.UploadFailed(p, &pb.OrchestrateRequest_UploadFailed{
					TargetPeers: targets,
				})
			}
		}
		log.Printf("New orchestate: %#v", o.scheduler)
	}
}

func (o *orchestration) hasOpenHandles() bool {
	// mtx is held
	for _, client := range o.connections {
		if client.hasOpenHandles {
			return true
		}
	}
	return false
}

func (o *orchestration) cleanupThread() {
	// Watch orchestration clients with open handles. Once there are no open
	// handles, close the orchestration after a minute.
	var deadline <-chan time.Time
	o.mtx.Lock()
	o.handlesChan <- struct{}{}
	defer o.mtx.Unlock()
	for !o.closing {
		o.mtx.Unlock()
		select {
		case <-o.handlesChan:
			o.mtx.Lock()
			if o.hasOpenHandles() {
				deadline = nil
			} else {
				deadline = time.After(60 * time.Second)
			}
		case <-deadline:
			o.mtx.Lock()
			o.close()
		}
	}
}

func (o *orchestration) close() {
	activeOrchestrationMtx.Lock()
	delete(activeOrchestration, o.activeDownload.DownloadId)
	activeOrchestrationMtx.Unlock()

	o.closing = true
	o.schedCond.Broadcast()
	for _, connection := range o.connections {
		connection.disconnecting = true
		connection.cond.Broadcast()
	}
}
