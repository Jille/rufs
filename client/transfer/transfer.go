package transfer

import (
	"context"
	"errors"
	"io"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/client/transfer/cache"
	"github.com/sgielen/rufs/client/transfer/orchestream"
	"github.com/sgielen/rufs/client/transfer/passive"
	"github.com/sgielen/rufs/common"
	"github.com/sgielen/rufs/intervals"
	pb "github.com/sgielen/rufs/proto"
)

func NewRemoteFile(ctx context.Context, filename, maybeHash string, size int64, peers []*connectivity.Peer) (*Transfer, error) {
	c, err := cache.New(size)
	if err != nil {
		return nil, err
	}
	t := &Transfer{
		storage:  c,
		filename: filename,
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

func NewLocalFile(filename, maybeHash string) (*Transfer, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	t := &Transfer{
		storage:  f,
		filename: filename,
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

func (t *Transfer) Read(ctx context.Context, offset int64, size int64) ([]byte, error) {
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
	if !t.have.Has(offset, offset+size) {
		t.want.Add(offset, offset+size)
		t.pending.Add(offset, offset+size)
		t.byteRangesUpdated()
		t.fetchCond.Broadcast()
		for {
			t.serveCond.Wait()
			if t.have.Has(offset, offset+size) {
				break
			}
			if !t.want.Has(offset, offset+size) {
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
				t.byteRangesUpdated()
				t.serveCond.Broadcast()
				break
			}
			if len(res.Data) == 0 {
				t.mtx.Lock()
				if res.GetRedirectToOrchestratedDownload() != 0 {
					if err := t.SwitchToOrchestratedMode(res.GetRedirectToOrchestratedDownload()); err != nil {
						log.Printf("Failed to switch to orchestrated mode: %v", err)
						t.want.Remove(offset, iv.End)
						t.byteRangesUpdated()
						t.serveCond.Broadcast()
					}
				}
				break
			}
			if _, err := t.storage.WriteAt(res.Data, offset); err != nil {
				t.mtx.Lock()
				log.Printf("ReadFile(%q): Write to cache failed: %v", t.filename, err)
				t.want.Remove(offset, iv.End)
				t.byteRangesUpdated()
				t.serveCond.Broadcast()
				break
			}
			t.receivedBytes(offset, offset+int64(len(res.Data)))
			offset += int64(len(res.Data))
			if res.GetRedirectToOrchestratedDownload() != 0 {
				if err := t.SwitchToOrchestratedMode(res.GetRedirectToOrchestratedDownload()); err != nil {
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
	t.byteRangesUpdated()
	t.serveCond.Broadcast()
	t.mtx.Unlock()
}

func (t *Transfer) SwitchToOrchestratedMode(downloadId int64) error {
	ctx := context.Background()
	circle := common.CircleFromPeer(t.peers[0].Name)
	pt := passive.New(ctx, t.storage, downloadId, passiveCallbacks{t})
	s, err := orchestream.New(ctx, circle, &pb.OrchestrateRequest_StartOrchestrationRequest{
		DownloadId: downloadId,
		Filename:   t.filename,
		Hash:       t.hash,
	}, pt)
	if err != nil {
		return err
	}
	t.mtx.Lock()
	t.orchestream = s
	t.quitFetchers = true
	t.killFetchers()
	t.fetchCond.Broadcast()
	t.byteRangesUpdated()
	t.mtx.Unlock()
	return nil
}

func (t *Transfer) DownloadId() int64 {
	return t.orchestream.DownloadId
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

func (t *Transfer) SetHash(hash string) {
	t.mtx.Lock()
	t.hash = hash
	// TODO: Poke orchestream
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
	}
	// TODO: close passive transfers
	t.mtx.Unlock()
	return t.storage.Close()
}
