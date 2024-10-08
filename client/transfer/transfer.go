package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Jille/rufs/client/connectivity"
	"github.com/Jille/rufs/client/metrics"
	"github.com/Jille/rufs/client/transfer/cache"
	"github.com/Jille/rufs/client/transfer/orchestream"
	"github.com/Jille/rufs/client/transfer/passive"
	"github.com/Jille/rufs/common"
	"github.com/Jille/rufs/intervals"
	pb "github.com/Jille/rufs/proto"
	"google.golang.org/grpc/status"
)

var (
	activeReadCounter int64

	ForgetCallback                  func(circle string, t *Transfer)
	RedirectToOrchestrationCallback func(circle string, t *Transfer, downloadId int64) error
)

func NewRemoteFile(ctx context.Context, remoteFilename, maybeHash string, size int64, peers []*connectivity.Peer) (_ *Transfer, retErr error) {
	defer func() {
		metrics.AddTransferOpens(connectivity.CirclesFromPeers(peers), status.Code(retErr).String(), 1)
	}()
	c, err := cache.New(size)
	if err != nil {
		return nil, err
	}
	t := &Transfer{
		circle:      common.CircleFromPeer(peers[0].Name),
		storage:     &replaceableBackend{storage: c, wg: &sync.WaitGroup{}},
		filename:    remoteFilename,
		hash:        maybeHash,
		size:        size,
		peers:       peers,
		handlesChan: make(chan int, 10),
	}
	rhSize := int64(1024)
	if rhSize > size {
		rhSize = size
	}
	t.readahead.Add(0, rhSize)
	t.init()
	fctx, cancel := context.WithCancel(context.Background())
	t.killFetchers = cancel
	go t.simpleFetcher(fctx)
	go t.simpleFetcher(fctx)
	return t, nil
}

func NewLocalFile(remoteFilename string, f *os.File, maybeHash, circle string) (*Transfer, error) {
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	t := &Transfer{
		circle:      circle,
		storage:     &replaceableBackend{storage: f, wg: &sync.WaitGroup{}},
		filename:    remoteFilename,
		hash:        maybeHash,
		size:        st.Size(),
		handlesChan: make(chan int, 10),
	}
	t.have.Add(0, st.Size())
	t.init()
	return t, nil
}

func (t *Transfer) SetLocalFile(f *os.File, maybeHash string) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	t.mtx.Lock()
	t.storage.replace(f)
	t.have.Add(0, st.Size())
	t.want = intervals.Intervals{}
	t.readahead = intervals.Intervals{}
	t.downloading = intervals.Intervals{}
	if maybeHash != "" {
		t.hash = maybeHash
	}
	t.quitFetchers = true
	t.killFetchers()
	t.serveCond.Broadcast()
	t.fetchCond.Broadcast()
	t.byteRangesUpdated()
	t.peers = nil
	t.size = st.Size()
	t.mtx.Unlock()
	return nil
}

type backend interface {
	io.ReaderAt
	io.WriterAt
	io.Closer
}

type Transfer struct {
	circle       string
	storage      *replaceableBackend
	filename     string
	hash         string
	size         int64
	peers        []*connectivity.Peer
	killFetchers func()
	handlesChan  chan int

	mtx          sync.Mutex
	handles      int
	closed       bool
	serveCond    *sync.Cond
	fetchCond    *sync.Cond
	have         intervals.Intervals
	want         intervals.Intervals
	readahead    intervals.Intervals
	downloading  intervals.Intervals
	quitFetchers bool
	orchestream  *orchestream.Stream
	oInitiator   bool
	passive      *passive.Transfer
}

type TransferHandle struct {
	transfer *Transfer
}

func (t *Transfer) init() {
	t.serveCond = sync.NewCond(&t.mtx)
	t.fetchCond = sync.NewCond(&t.mtx)
	t.killFetchers = func() {}

	go t.openHandlesWatcher()
	go func() {
		t.mtx.Lock()
		for !t.closed {
			t.mtx.Unlock()
			time.Sleep(time.Second)
			downloadId := t.DownloadId()
			t.mtx.Lock()
			log.Printf("Klikspaan{%d}: have: %v; want: %v; ahead: %v; downloading: %v", downloadId, t.have.Export(), t.want.Export(), t.readahead.Export(), t.downloading.Export())
		}
		t.mtx.Unlock()
	}()
}

func (t *Transfer) openHandlesWatcher() {
	// Watch open handles. Keep the orchestrator informed whether this peer
	// has handles open. Once there are no open handles and no orchestrator, close
	// the transfer after a minute.
	var deadline <-chan time.Time
	t.mtx.Lock()
	t.handlesChan <- t.handles
	defer t.mtx.Unlock()
	for !t.closed {
		t.mtx.Unlock()
		select {
		case handles := <-t.handlesChan:
			t.mtx.Lock()
			if handles == 0 && t.orchestream == nil {
				deadline = time.After(60 * time.Second)
			} else {
				deadline = nil
			}
			if t.orchestream != nil {
				t.orchestream.SetHaveHandles(handles > 0)
			}
		case <-deadline:
			t.mtx.Lock()
			t.close()
		}
	}
}

func (t *Transfer) TransferIsRemote() bool {
	return len(t.peers) > 0
}

func (t *Transfer) GetHandle() *TransferHandle {
	t.mtx.Lock()
	if t.closed {
		log.Panicf("GetHandle on a closed Transfer")
	}
	defer t.mtx.Unlock()
	t.handles += 1
	t.handlesChan <- t.handles
	return &TransferHandle{t}
}

func (h *TransferHandle) Close() error {
	t := h.transfer
	if t == nil {
		log.Panicf("close on a closed TransferHandle")
	}
	t.mtx.Lock()
	defer t.mtx.Unlock()
	t.handles -= 1
	t.handlesChan <- h.transfer.handles
	t = nil
	return nil
}

func (h *TransferHandle) ReadAt(buf []byte, offset int64) (n int, retErr error) {
	const MIN_READAHEAD_SIZE = 1024 * 32
	t := h.transfer
	if t == nil {
		log.Panicf("read on a closed TransferHandle")
	}

	size := int64(len(buf))
	startTime := time.Now()
	var recvBytes int64
	metrics.SetTransferReadsActive(connectivity.CirclesFromPeers(t.peers), atomic.AddInt64(&activeReadCounter, 1))
	defer func() {
		circles := connectivity.CirclesFromPeers(t.peers)
		code := status.Code(retErr).String()
		latency := time.Since(startTime).Seconds()
		kbytes := fmt.Sprintf("%.0f", math.Ceil(float64(recvBytes)/1024))
		metrics.AddTransferReads(circles, code, 1)
		metrics.AppendTransferReadSizes(circles, float64(size))
		metrics.AppendTransferReadLatency(circles, code, kbytes, latency)
		metrics.SetTransferReadsActive(connectivity.CirclesFromPeers(t.peers), atomic.AddInt64(&activeReadCounter, -1))
	}()
	if offset >= t.size {
		return 0, io.EOF
	}
	if offset+size > t.size {
		size = t.size - offset
	}
	if size == 0 {
		return 0, nil
	}
	// sizeAhead is the read-ahead size after offset+size. By default, we set it
	// to the same size as the requested block, assuming the next Read call will
	// ask for an equal-sized block. Readahead blocks are sent to clients with a
	// lower priority than readnow blocks.
	sizeAhead := size
	if sizeAhead < MIN_READAHEAD_SIZE {
		// If the size is very small, read ahead a little further, since the client
		// may ask for it sooner
		sizeAhead = MIN_READAHEAD_SIZE
	}
	if offset+size+sizeAhead > t.size {
		sizeAhead = t.size - (offset + size)
	}
	t.mtx.Lock()
	missing := t.have.FindUncovered(offset, offset+size)
	missingAhead := t.have.FindUncovered(offset+size, offset+size+sizeAhead)
	if !missing.IsEmpty() || !missingAhead.IsEmpty() {
		for _, m := range missing.Export() {
			recvBytes += m.End - m.Start
		}
		t.want.AddRange(missing)
		t.readahead.AddRange(missingAhead)
		// Prevent overlap between want & readahead
		t.readahead.RemoveRange(t.want)
		t.byteRangesUpdated()
		t.fetchCond.Broadcast()
		for !missing.IsEmpty() {
			t.serveCond.Wait()
			missing = t.have.FindUncovered(offset, offset+size)
			missingWant := t.want.FindUncoveredRange(missing)
			if !missingWant.IsEmpty() {
				t.mtx.Unlock()
				return 0, errors.New("simpleFetcher admitted failure")
			}
		}
	}
	t.mtx.Unlock()
	return t.storage.ReadAt(buf, offset)
}

func (t *Transfer) simpleFetcher(ctx context.Context) {
	const MAX_DOWNLOAD_SIZE = 1024 * 128
	t.mtx.Lock()
	pno := rand.Intn(len(t.peers))
	for {
		// We should hold t.mtx at the start of each iteration.
		var iv intervals.Interval
		for {
			if t.quitFetchers {
				t.mtx.Unlock()
				return
			}
			needsDownload := t.downloading.FindUncoveredRange(t.want)
			if needsDownload.IsEmpty() {
				needsDownload = t.downloading.FindUncoveredRange(t.readahead)
			}
			if !needsDownload.IsEmpty() {
				iv = needsDownload.Export()[0]
				if iv.Size() > MAX_DOWNLOAD_SIZE {
					iv.End = iv.Start + MAX_DOWNLOAD_SIZE
				}
				break
			}
			t.fetchCond.Wait()
		}
		t.downloading.Add(iv.Start, iv.End)
		t.mtx.Unlock()
		pno = (pno + 1) % len(t.peers)
		stream, err := t.peers[pno].ContentServiceClient().ReadFile(ctx, &pb.ReadFileRequest{
			Filename: t.filename,
			Offset:   iv.Start,
			Rdnow:    iv.End - iv.Start,
			Rdahead:  0,
		})
		if err != nil {
			t.mtx.Lock()
			log.Printf("ReadFile(%q) from %s failed: %v", t.filename, t.peers[pno].Name, err)
			t.want.Remove(iv.Start, iv.End)
			t.readahead.Remove(iv.Start, iv.End)
			t.downloading.Remove(iv.Start, iv.End)
			t.serveCond.Broadcast()
			continue
		}
		offset := iv.Start
		for {
			// We're not holding any locks.
			res, err := stream.Recv()
			if err != nil {
				t.mtx.Lock()
				if offset < iv.End {
					t.downloading.Remove(offset, iv.End)
				}
				if err != io.EOF && !t.quitFetchers {
					t.want.Remove(offset, iv.End)
					t.readahead.Remove(offset, iv.End)
					log.Printf("ReadFile(%q).Recv() from %s failed: %v", t.filename, t.peers[pno].Name, err)
				}
				t.byteRangesUpdated()
				t.serveCond.Broadcast()
				break
			}
			if len(res.Data) > 0 {
				if _, err := t.storage.WriteAt(res.Data, offset); err != nil {
					t.mtx.Lock()
					if !strings.Contains(err.Error(), "bad file descriptor") {
						// This happens after SetLocalFile() was called, at which point t.storage becomes readonly.
						// This is harmless.
						log.Printf("ReadFile(%q): Write to cache failed: %v", t.filename, err)
					}
					t.want.Remove(offset, iv.End)
					t.readahead.Remove(offset, iv.End)
					t.downloading.Remove(iv.Start, iv.End)
					t.byteRangesUpdated()
					t.serveCond.Broadcast()
					break
				}
				t.receivedBytes(offset, offset+int64(len(res.Data)), "simple", t.peers[pno].Name)
				offset += int64(len(res.Data))
			}
			if downloadId := res.GetRedirectToOrchestratedDownload(); downloadId != 0 {
				if err := RedirectToOrchestrationCallback(t.circle, t, downloadId); err != nil {
					log.Printf("Failed to switch to orchestrated mode (continuing in simple mode): %v", err)
				}
			}
		}
	}
}

func (t *Transfer) receivedBytes(start, end int64, transferType string, peer string) {
	t.mtx.Lock()
	t.have.Add(start, end)
	t.want.Remove(start, end)
	t.readahead.Remove(start, end)
	t.downloading.Remove(start, end)
	t.byteRangesUpdated()
	t.serveCond.Broadcast()
	t.mtx.Unlock()
	metrics.AddTransferRecvBytes([]string{t.circle}, peer, transferType, end-start)
}

func (t *Transfer) SwitchToOrchestratedMode(downloadId int64) error {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if t.orchestream != nil && t.orchestream.DownloadId == downloadId {
		return nil
	}

	initiator := downloadId == 0
	ctx := context.Background()
	pt := passive.New(ctx, t.storage, downloadId, passiveCallbacks{t})
	s, err := orchestream.New(ctx, t.circle, &pb.OrchestrateRequest_StartOrchestrationRequest{
		DownloadId: downloadId,
		Filename:   t.filename,
		Hash:       t.hash,
	}, pt)
	if err != nil {
		return err
	}
	t.orchestream = s
	t.handlesChan <- t.handles
	t.oInitiator = initiator
	t.passive = pt
	t.quitFetchers = true
	t.killFetchers()
	t.fetchCond.Broadcast()
	t.byteRangesUpdated()
	s.SetHaveHandles(t.handles > 0)
	return nil
}

func (t *Transfer) switchFromOrchestratedMode() {
	t.mtx.Lock()
	t.orchestream.Close()
	t.orchestream = nil
	t.handlesChan <- t.handles
	t.passive.Close()
	t.passive = nil
	t.quitFetchers = false
	if t.TransferIsRemote() {
		fctx, cancel := context.WithCancel(context.Background())
		t.killFetchers = cancel
		go t.simpleFetcher(fctx)
		go t.simpleFetcher(fctx)
	}
	t.mtx.Unlock()
}

func (t *Transfer) DownloadId() int64 {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if t.orchestream == nil {
		return 0
	}
	return t.orchestream.DownloadId
}

func (t *Transfer) HandleIncomingPassiveTransfer(stream pb.ContentService_PassiveTransferServer) error {
	return t.passive.HandleIncomingPassiveTransfer(stream)
}

func (t *Transfer) byteRangesUpdated() {
	if t.orchestream == nil {
		return
	}
	t.orchestream.SetByteRanges(&pb.OrchestrateRequest_UpdateByteRanges{
		Have:      intervalsToRanges(t.have),
		Readnow:   intervalsToRanges(t.want),
		Readahead: intervalsToRanges(t.readahead),
	})
}

func intervalsToRanges(ivs intervals.Intervals) []*pb.Range {
	l := ivs.Export()
	ret := make([]*pb.Range, len(l))
	for i, iv := range l {
		ret[i] = &pb.Range{
			Start: iv.Start,
			End:   iv.End,
		}
	}
	return ret
}

type passiveCallbacks struct {
	t *Transfer
}

func (pc passiveCallbacks) ReceivedBytes(start, end int64, peer string) {
	pc.t.receivedBytes(start, end, "passive", peer)
}

func (pc passiveCallbacks) UploadFailed(peer string) {
	pc.t.mtx.Lock()
	if pc.t.orchestream != nil {
		pc.t.orchestream.UploadFailed(peer)
	}
	pc.t.mtx.Unlock()
}

func (pc passiveCallbacks) SetConnectedPeers(peers []string) {
	pc.t.mtx.Lock()
	if pc.t.orchestream != nil {
		pc.t.orchestream.SetConnectedPeers(peers)
	}
	pc.t.mtx.Unlock()
}

func (pc passiveCallbacks) OrchestrationClosed() {
	pc.t.switchFromOrchestratedMode()
}

func (t *Transfer) GetHash() string {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	return t.hash
}

func (t *Transfer) SetHash(hash string) {
	t.mtx.Lock()
	if t.hash != "" && t.hash != hash {
		log.Fatalf("File hash changed for remote file %s", t.filename)
	}
	newHash := t.hash == ""
	t.hash = hash
	if t.orchestream != nil && t.oInitiator && newHash {
		t.orchestream.SetHash(hash)
	}
	t.mtx.Unlock()
}

func (t *Transfer) close() error {
	// t.mtx is held
	ForgetCallback(t.circle, t)

	if t.handles > 0 {
		panic("Closing Transfer while handles are still open")
	}
	t.closed = true
	t.quitFetchers = true
	t.killFetchers()
	t.want = intervals.Intervals{}
	t.readahead = intervals.Intervals{}
	t.downloading = intervals.Intervals{}
	t.fetchCond.Broadcast()
	t.serveCond.Broadcast()
	if t.orchestream != nil {
		t.orchestream.Close()
		t.passive.Close()
	}
	return t.storage.Close()
}

type replaceableBackend struct {
	mtx     sync.Mutex
	wg      *sync.WaitGroup
	storage backend
}

func (r *replaceableBackend) WriteAt(p []byte, off int64) (n int, err error) {
	r.mtx.Lock()
	s := r.storage
	wg := r.wg
	wg.Add(1)
	defer wg.Done()
	r.mtx.Unlock()
	return s.WriteAt(p, off)
}

func (r *replaceableBackend) ReadAt(p []byte, off int64) (n int, err error) {
	r.mtx.Lock()
	s := r.storage
	wg := r.wg
	wg.Add(1)
	defer wg.Done()
	r.mtx.Unlock()
	return s.ReadAt(p, off)
}

func (r *replaceableBackend) Close() error {
	r.mtx.Lock()
	s := r.storage
	r.mtx.Unlock()
	return s.Close()
}

func (r *replaceableBackend) replace(s backend) {
	r.mtx.Lock()
	old := r.storage
	r.storage = s
	wg := r.wg
	r.wg = &sync.WaitGroup{}
	r.mtx.Unlock()
	go func() {
		wg.Wait()
		if err := old.Close(); err != nil {
			log.Printf("Failed to close old replaceableBackend: %v", err)
		}
	}()
}
