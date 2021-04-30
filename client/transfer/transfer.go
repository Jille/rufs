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
	"sync"
	"sync/atomic"
	"time"

	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/client/metrics"
	"github.com/sgielen/rufs/client/transfer/cache"
	"github.com/sgielen/rufs/client/transfer/orchestream"
	"github.com/sgielen/rufs/client/transfer/passive"
	"github.com/sgielen/rufs/common"
	"github.com/sgielen/rufs/intervals"
	pb "github.com/sgielen/rufs/proto"
	"google.golang.org/grpc/status"
)

var (
	activeReadCounter int64

	activeMtx       sync.Mutex
	activeTransfers = map[string]map[string]*Transfer{}
)

func GetTransferForDownloadId(circle string, downloadId int64) *Transfer {
	activeMtx.Lock()
	defer activeMtx.Unlock()
	for _, t := range activeTransfers[circle] {
		if t.DownloadId() == downloadId {
			return t
		}
	}
	return nil
}

func GetActiveTransfer(circle, remoteFilename string) *Transfer {
	activeMtx.Lock()
	defer activeMtx.Unlock()
	return activeTransfers[circle][remoteFilename]
}

func getOrCreateActiveTransfer(circle, remoteFilename string, create func() (*Transfer, error)) (*Transfer, error) {
	activeMtx.Lock()
	defer activeMtx.Unlock()
	if t, ok := activeTransfers[circle][remoteFilename]; ok {
		return t, nil
	}
	if _, found := activeTransfers[circle]; !found {
		activeTransfers[circle] = map[string]*Transfer{}
	}
	t, err := create()
	if err != nil {
		return nil, err
	}
	activeTransfers[circle][remoteFilename] = t
	return t, nil
}

func OpenRemoteFile(ctx context.Context, remoteFilename, maybeHash string, size int64, peers []*connectivity.Peer) (_ *Transfer, retErr error) {
	return getOrCreateActiveTransfer(common.CircleFromPeer(peers[0].Name), remoteFilename, func() (*Transfer, error) {
		return newRemoteFile(ctx, remoteFilename, maybeHash, size, peers)
	})
}

func OpenLocalFile(remoteFilename, localFilename, maybeHash, circle string) (*Transfer, error) {
	return getOrCreateActiveTransfer(circle, remoteFilename, func() (*Transfer, error) {
		return newLocalFile(remoteFilename, localFilename, maybeHash, circle)
	})
}

func newRemoteFile(ctx context.Context, remoteFilename, maybeHash string, size int64, peers []*connectivity.Peer) (_ *Transfer, retErr error) {
	defer func() {
		metrics.AddTransferOpens(connectivity.CirclesFromPeers(peers), status.Code(retErr).String(), 1)
	}()
	c, err := cache.New(size)
	if err != nil {
		return nil, err
	}
	t := &Transfer{
		circle:   common.CircleFromPeer(peers[0].Name),
		storage:  c,
		filename: remoteFilename,
		hash:     maybeHash,
		size:     size,
		peers:    peers,
	}
	t.readahead.Add(0, 1024)
	t.init()
	fctx, cancel := context.WithCancel(context.Background())
	t.killFetchers = cancel
	go t.simpleFetcher(fctx)
	go t.simpleFetcher(fctx)
	return t, nil
}

func newLocalFile(remoteFilename, localFilename, maybeHash, circle string) (*Transfer, error) {
	f, err := os.Open(localFilename)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	t := &Transfer{
		circle:   circle,
		storage:  f,
		filename: remoteFilename,
		hash:     maybeHash,
		size:     st.Size(),
	}
	t.have.Add(0, st.Size())
	t.init()
	return t, nil
}

type backend interface {
	io.ReaderAt
	io.WriterAt
	io.Closer
}

type Transfer struct {
	circle       string
	storage      backend
	filename     string
	hash         string
	size         int64
	peers        []*connectivity.Peer
	killFetchers func()

	mtx          sync.Mutex
	serveCond    *sync.Cond
	fetchCond    *sync.Cond
	have         intervals.Intervals
	want         intervals.Intervals
	readahead    intervals.Intervals
	pending      intervals.Intervals
	quitFetchers bool
	orchestream  *orchestream.Stream
	passive      *passive.Transfer
}

func (t *Transfer) init() {
	t.serveCond = sync.NewCond(&t.mtx)
	t.fetchCond = sync.NewCond(&t.mtx)
	t.killFetchers = func() {}
	go func() {
		for {
			time.Sleep(time.Second)
			t.mtx.Lock()
			log.Printf("Klikspaan: want: %v; have: %v", t.want.Export(), t.have.Export())
			t.mtx.Unlock()
		}
	}()
}

func (t *Transfer) Read(ctx context.Context, offset int64, size int64) (_ []byte, retErr error) {
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
		return nil, io.EOF
	}
	if offset+size > t.size {
		size = t.size - offset
	}
	if size == 0 {
		return nil, nil
	}
	t.mtx.Lock()
	missing := t.have.FindUncovered(offset, offset+size)
	if !missing.IsEmpty() {
		for _, m := range missing.Export() {
			recvBytes += m.End - m.Start
		}
		t.want.AddRange(missing)
		t.pending.AddRange(missing)
		t.byteRangesUpdated()
		t.fetchCond.Broadcast()
		for {
			t.serveCond.Wait()
			missing = t.have.FindUncovered(offset, offset+size)
			if missing.IsEmpty() {
				break
			}
			missingWant := t.want.FindUncoveredRange(missing)
			if !missingWant.IsEmpty() {
				t.mtx.Unlock()
				return nil, errors.New("simpleFetcher admitted failure")
			}
		}
	}
	t.mtx.Unlock()
	buf := make([]byte, size)
	_, err := t.storage.ReadAt(buf, offset)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func (t *Transfer) simpleFetcher(ctx context.Context) {
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
			p := t.pending.Export()
			if len(p) > 0 {
				iv = p[0]
				t.pending.Remove(iv.Start, iv.End)
				break
			}
			t.fetchCond.Wait()
		}
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
			t.serveCond.Broadcast()
			continue
		}
		offset := iv.Start
		for {
			// We're not holding any locks.
			res, err := stream.Recv()
			if err != nil {
				t.mtx.Lock()
				if err == io.EOF || t.quitFetchers {
					break
				}
				log.Printf("ReadFile(%q).Recv() from %s failed: %v", t.filename, t.peers[pno].Name, err)
				t.want.Remove(offset, iv.End)
				t.readahead.Remove(offset, iv.End)
				t.byteRangesUpdated()
				t.serveCond.Broadcast()
				break
			}
			if len(res.Data) > 0 {
				if _, err := t.storage.WriteAt(res.Data, offset); err != nil {
					t.mtx.Lock()
					log.Printf("ReadFile(%q): Write to cache failed: %v", t.filename, err)
					t.want.Remove(offset, iv.End)
					t.readahead.Remove(offset, iv.End)
					t.byteRangesUpdated()
					t.serveCond.Broadcast()
					break
				}
				t.receivedBytes(offset, offset+int64(len(res.Data)))
				offset += int64(len(res.Data))
			}
			if downloadId := res.GetRedirectToOrchestratedDownload(); downloadId != 0 {
				if err := t.SwitchToOrchestratedMode(downloadId); err != nil {
					log.Printf("Failed to switch to orchestrated mode (continuing in simple mode): %v", err)
				}
			}
		}
	}
}

func (t *Transfer) receivedBytes(start, end int64) {
	t.mtx.Lock()
	t.have.Add(start, end)
	t.want.Remove(start, end)
	t.readahead.Remove(start, end)
	t.byteRangesUpdated()
	t.serveCond.Broadcast()
	t.mtx.Unlock()
}

func (t *Transfer) SwitchToOrchestratedMode(downloadId int64) error {
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
	t.mtx.Lock()
	t.orchestream = s
	t.passive = pt
	t.quitFetchers = true
	t.killFetchers()
	t.fetchCond.Broadcast()
	t.byteRangesUpdated()
	t.mtx.Unlock()
	return nil
}

func (t *Transfer) DownloadId() int64 {
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

func (pc passiveCallbacks) ReceivedBytes(start, end int64) {
	pc.t.receivedBytes(start, end)
}

func (pc passiveCallbacks) UploadFailed(peer string) {
	pc.t.orchestream.UploadFailed(peer)
}

func (pc passiveCallbacks) SetConnectedPeers(peers []string) {
	pc.t.orchestream.SetConnectedPeers(peers)
}

func (t *Transfer) GetHash() string {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	return t.hash
}

func (t *Transfer) SetHash(hash string) {
	t.mtx.Lock()
	t.hash = hash
	if t.orchestream != nil {
		t.orchestream.SetHash(hash)
	}
	t.mtx.Unlock()
}

func (t *Transfer) Close() error {
	t.mtx.Lock()
	t.quitFetchers = true
	t.killFetchers()
	t.want = intervals.Intervals{}
	t.readahead = intervals.Intervals{}
	t.pending = intervals.Intervals{}
	t.fetchCond.Broadcast()
	t.serveCond.Broadcast()
	if t.orchestream != nil {
		t.orchestream.Close()
		t.passive.Close()
	}
	t.mtx.Unlock()
	return t.storage.Close()
}
