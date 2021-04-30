// Package passive opens PassiveTransfer streams to all given peers, handles uploads and downloads and reports back updates.
package passive

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/sgielen/rufs/client/connectivity"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"google.golang.org/grpc"
)

type backend interface {
	io.ReaderAt
	io.WriterAt
	io.Closer
}

type TransferClient interface {
	ReceivedBytes(start, end int64)
	SetConnectedPeers(peers []string)
	UploadFailed(peer string)
}

func New(ctx context.Context, storage backend, downloadId int64, callbacks TransferClient) *Transfer {
	ctx, cancel := context.WithCancel(ctx)
	return &Transfer{
		ctx:        ctx,
		cancel:     cancel,
		storage:    storage,
		downloadId: downloadId,
		callbacks:  callbacks,
		peers:      map[string]*peer{},
	}
}

type Transfer struct {
	ctx        context.Context
	cancel     func()
	storage    backend
	downloadId int64
	callbacks  TransferClient

	mtx   sync.Mutex
	peers map[string]*peer
}

type peer struct {
	transfer *Transfer
	name     string

	mtx                  sync.Mutex
	cond                 *sync.Cond
	pendingTransmissions []*pb.Range
	activeSenders        int
	connectedStreams     int
}

func (t *Transfer) setConnectedPeers() {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	var peers []string
	for name, p := range t.peers {
		p.mtx.Lock()
		if p.connectedStreams > 0 {
			peers = append(peers, name)
		}
		p.mtx.Unlock()
	}
	t.callbacks.SetConnectedPeers(peers)
}

func (t *Transfer) Welcome(downloadId int64) {
	t.downloadId = downloadId
}

func (t *Transfer) SetPeers(ctx context.Context, peers []string) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	for _, p := range peers {
		if _, found := t.peers[p]; found {
			continue
		}
		t.addPeer(p)
	}
}

func (t *Transfer) addPeer(name string) *peer {
	// t.mtx must be held
	pe := &peer{
		transfer: t,
		name:     name,
	}
	pe.cond = sync.NewCond(&pe.mtx)
	t.peers[name] = pe
	go pe.connectLoop()
	return pe
}

func (p *peer) connectLoop() {
	for {
		err := p.connectAndHandle()
		log.Printf("PassiveTransfer(%s) reconnecting: %v", p.name, err)
		select {
		case <-p.transfer.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (p *peer) connectAndHandle() error {
	ctx, cancel := context.WithCancel(p.transfer.ctx)
	defer cancel()
	peer := connectivity.GetPeer(p.name)
	if peer == nil {
		return fmt.Errorf("attempted to connect to peer {%s}, but it isn't known", p.name)
	}
	stream, err := peer.ContentServiceClient().PassiveTransfer(ctx, grpc.WaitForReady(true))
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.PassiveTransferData{
		DownloadId: p.transfer.downloadId,
	}); err != nil {
		return err
	}
	return p.handleStream(stream)
}

func (p *peer) handleStream(stream PassiveStream) error {
	errCh := make(chan error, 2)
	quit := make(chan struct{})
	go func() {
		errCh <- p.transmitDataLoopManager(stream, quit)
	}()
	go func() {
		errCh <- p.transfer.handleInboundData(stream)
	}()
	p.mtx.Lock()
	p.connectedStreams++
	p.mtx.Unlock()

	p.transfer.setConnectedPeers()

	err := <-errCh
	close(quit)

	p.mtx.Lock()
	p.connectedStreams--
	p.cond.Broadcast()
	p.mtx.Unlock()

	p.transfer.setConnectedPeers()

	return err
}

func (t *Transfer) handleInboundData(stream PassiveStream) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		if _, err = t.storage.WriteAt(msg.GetData(), msg.GetOffset()); err != nil {
			return err
		}
		t.callbacks.ReceivedBytes(msg.GetOffset(), msg.GetOffset()+int64(len(msg.GetData())))
	}
}

func (p *peer) transmitDataLoopManager(stream PassiveStream, quit chan struct{}) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.activeSenders++
	internalError, err := p.transmitDataLoop(stream, quit)
	p.activeSenders--
	if len(p.pendingTransmissions) != 0 && (p.activeSenders == 0 || internalError) {
		// We can't send to this peer anymore, or an internal (cache) error occurred; don't
		// send pending transmissions anymore and signal to orchestrator that the remaining
		// ones failed
		p.transfer.callbacks.UploadFailed(p.name)
		p.pendingTransmissions = nil
	}
	return err
}

func (p *peer) transmitDataLoop(stream PassiveStream, quit chan struct{}) (internalError bool, err error) {
	for {
		for len(p.pendingTransmissions) > 0 {
			select {
			case <-quit:
				return false, nil
			default:
			}
			task := p.pendingTransmissions[0]
			p.pendingTransmissions = p.pendingTransmissions[1:]
			p.mtx.Unlock()
			offset, internalError, err := p.upload(stream, task)
			p.mtx.Lock()
			if err != nil {
				p.pendingTransmissions = append([]*pb.Range{{Start: offset, End: task.End}}, p.pendingTransmissions...)
				return internalError, err
			}
		}
		select {
		case <-quit:
			return false, nil
		default:
		}
		p.cond.Wait()
	}
}

func (p *peer) upload(stream PassiveStream, task *pb.Range) (remainingOffset int64, internalError bool, err error) {
	var buf [8192]byte
	offset := task.GetStart()
	for {
		s := task.GetEnd() - offset
		if s > int64(len(buf)) {
			s = int64(len(buf))
		}
		if s <= 0 {
			return offset, false, nil
		}
		n, err := p.transfer.storage.ReadAt(buf[:s], offset)
		if err != nil {
			return offset, true, err
		}
		if err := stream.Send(&pb.PassiveTransferData{
			Data:   buf[:n],
			Offset: offset,
		}); err != nil {
			return offset, false, err
		}
		offset += int64(n)
	}
}

func (t *Transfer) HandleIncomingPassiveTransfer(stream pb.ContentService_PassiveTransferServer) error {
	name, _, err := security.PeerFromContext(stream.Context())
	if err != nil {
		return err
	}
	t.mtx.Lock()
	p, ok := t.peers[name]
	if !ok {
		p = t.addPeer(name)
	}
	t.mtx.Unlock()
	return p.handleStream(stream)
}

func (t *Transfer) Upload(ctx context.Context, peer string, byteRange *pb.Range) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	p, ok := t.peers[peer]
	if !ok || p.connectedStreams == 0 {
		log.Printf("Requested upload failed: peer not known or not connected")
		t.callbacks.UploadFailed(peer)
		return
	}
	p.pendingTransmissions = append(p.pendingTransmissions, byteRange)
	p.cond.Broadcast()
}

func (t *Transfer) Close() error {
	t.cancel()
	return nil
}

type PassiveStream interface {
	Recv() (*pb.PassiveTransferData, error)
	Send(*pb.PassiveTransferData) error
}
