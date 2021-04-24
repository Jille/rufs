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

	mtx       sync.Mutex
	serveCond *sync.Cond
	fetchCond *sync.Cond
	have      intervals.Intervals
	want      intervals.Intervals
	readahead intervals.Intervals
	quit      bool
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
			if t.quit {
				t.mtx.Unlock()
				return
			}
			want := t.want.Export()
			if len(want) > 0 {
				iv = want[0]
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
				if err == io.EOF || t.quit {
					break
				}
				log.Printf("ReadFile(%q).Recv() from %s failed: %v", t.filename, t.peers[pno].Name, err)
				t.want.Remove(offset, iv.End)
				t.serveCond.Broadcast()
				break
			}
			if len(res.Data) == 0 {
				t.mtx.Lock()
				break
			}
			if _, err := t.storage.WriteAt(res.Data, offset); err != nil {
				t.mtx.Lock()
				log.Printf("ReadFile(%q): Write to cache failed: %v", t.filename, err)
				t.want.Remove(offset, iv.End)
				t.serveCond.Broadcast()
				break
			}
			t.mtx.Lock()
			t.have.Add(offset, offset+int64(len(res.Data)))
			t.want.Remove(offset, offset+int64(len(res.Data)))
			t.serveCond.Broadcast()
			t.mtx.Unlock()
			offset += int64(len(res.Data))
		}
	}
}

func (t *Transfer) Close() error {
	t.mtx.Lock()
	t.quit = true
	t.killFetchers()
	t.want = intervals.Intervals{}
	t.readahead = intervals.Intervals{}
	t.fetchCond.Broadcast()
	t.serveCond.Broadcast()
	t.mtx.Unlock()
	return t.storage.Close()
}
