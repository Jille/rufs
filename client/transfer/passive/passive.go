// Package passive opens PassiveTransfer streams to all given peers, handles uploads and downloads and reports back updates.
package passive

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"sync/atomic"
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
	}
}

type Transfer struct {
	ctx        context.Context
	cancel     func()
	storage    backend
	downloadId int64
	callbacks  TransferClient

	mtx            sync.Mutex
	connectedPeers map[string]*peer
}

type peer struct {
	transfer *Transfer
	name     string

	mtx                  sync.Mutex
	cond                 *sync.Cond
	pendingTransmissions []*pb.Range
	activeSenders        int
}

func (t *Transfer) SetPeers(ctx context.Context, peers []string) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	for _, p := range peers {
		if _, found := t.connectedPeers[p]; found {
			continue
		}
		pe := &peer{
			transfer: t,
			name:     p,
		}
		pe.cond = sync.NewCond(&pe.mtx)
		t.connectedPeers[p] = pe
		go pe.connectLoop()
	}
}

func (p *peer) connectLoop() {
	for {
		err := p.connect()
		log.Printf("PassiveTransfer(%s) error: %v", p.name, err)
		select {
		case <-p.transfer.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (p *peer) connect() error {
	stream, err := connectivity.GetPeer(p.name).ContentServiceClient().PassiveTransfer(p.transfer.ctx, grpc.WaitForReady(true))
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
	dead := int32(0)
	defer atomic.StoreInt32(&dead, 1)
	go p.transmitDataLoop(stream, &dead)
	return p.transfer.handleInboundData(stream)
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

func (p *peer) transmitDataLoop(stream PassiveStream, dead *int32) error {
	var buf [8192]byte
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.activeSenders++
	fail := func(work *pb.Range) {
		p.mtx.Lock()
		defer p.mtx.Unlock()
		p.activeSenders--
		d := atomic.LoadInt32(dead) != 0
		if p.activeSenders == 0 || work != nil {
			if !d {
				p.transfer.callbacks.UploadFailed(p.name)
				p.pendingTransmissions = nil
			} else if work != nil {
				p.pendingTransmissions = append([]*pb.Range{work}, p.pendingTransmissions...)
			}
		}
	}
	for {
		if atomic.LoadInt32(dead) != 0 {
			fail(nil)
			return nil
		}
		for len(p.pendingTransmissions) > 0 {
			task := p.pendingTransmissions[0]
			p.pendingTransmissions = p.pendingTransmissions[1:]
			p.mtx.Unlock()
			l := task.GetEnd() - task.GetStart()
			n, err := p.transfer.storage.ReadAt(buf[:l], task.GetStart())
			if err != nil {
				fail(task)
				return err
			}
			if err := stream.Send(&pb.PassiveTransferData{
				Data:   buf[:n],
				Offset: task.GetStart(),
			}); err != nil {
				fail(task)
				return err
			}
			p.mtx.Lock()
			if atomic.LoadInt32(dead) != 0 {
				fail(nil)
				return nil
			}
		}
		p.cond.Wait()
	}
}

func (t *Transfer) HandleIncomingPassiveTransfer(stream pb.ContentService_PassiveTransferServer) error {
	peer, _, err := security.PeerFromContext(stream.Context())
	if err != nil {
		return err
	}
	t.mtx.Lock()
	p, ok := t.connectedPeers[peer]
	t.mtx.Unlock()
	if !ok {
		// TODO: might as well just add them as a connected peer; we'll get them from the PeerList soon enough.
		return errors.New("you're not in this orchestration")
	}
	return p.handleStream(stream)
}

func (t *Transfer) Upload(ctx context.Context, peer string, byteRange *pb.Range) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	p, ok := t.connectedPeers[peer]
	if !ok {
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
