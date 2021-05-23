package vfs

import (
	"container/heap"
	"sync"
	"time"

	"github.com/sgielen/rufs/client/connectivity"
	pb "github.com/sgielen/rufs/proto"
)

var (
	cacheAgeExpired       int
	cacheAgeRecent        int
	vfsCacheEntriesTarget int

	cacheMtx        sync.Mutex
	cleanNow        chan struct{}
	vfsCache        map[string]map[string]*cacheEntry
	vfsCacheEntries int
)

type cacheEntry struct {
	response *pb.ReadDirResponse
	err      error
	recvtime int64
}

func InitCache(target int) {
	cacheAgeExpired = 30
	cacheAgeRecent = 10
	vfsCacheEntriesTarget = target

	cleanNow = make(chan struct{}, 1)
	vfsCache = make(map[string]map[string]*cacheEntry)
	vfsCacheEntries = 0

	if cacheIsEnabled() {
		go cacheCleaner()
	}
}

func cacheIsEnabled() bool {
	return vfsCacheEntriesTarget > 0
}

func cacheCleaner() {
	for {
		select {
		case <-cleanNow:
		case <-time.After(time.Second * 30):
		}
		cacheMtx.Lock()
		for path, pathcache := range vfsCache {
			for peer, entry := range pathcache {
				age := int(time.Now().Unix() - entry.recvtime)
				if age < 0 || age > cacheAgeExpired {
					dropCache(path, peer)
				}
			}
		}
		if vfsCacheEntries > vfsCacheEntriesTarget {
			// Shrink cache to 90% of target size
			numToRemove := vfsCacheEntries - int(0.9*float64(vfsCacheEntriesTarget))
			aggressiveCacheClean(numToRemove)
		}
		cacheMtx.Unlock()
	}
}

func dropCache(path string, peer string) {
	// mtx is held
	// cache entry is assumed to exist
	delete(vfsCache[path], peer)
	if len(vfsCache[path]) == 0 {
		delete(vfsCache, path)
	}
	vfsCacheEntries -= 1
}

func putCache(req *pb.ReadDirRequest, peer *connectivity.Peer, response *pb.ReadDirResponse, err error) {
	if !cacheIsEnabled() {
		return
	}

	cacheMtx.Lock()
	defer cacheMtx.Unlock()

	path := req.GetPath()
	if vfsCache[path] == nil {
		vfsCache[path] = map[string]*cacheEntry{}
	}
	if vfsCache[path][peer.Name] == nil {
		vfsCacheEntries += 1
		vfsCache[path][peer.Name] = &cacheEntry{}
	}

	entry := vfsCache[path][peer.Name]
	entry.err = err
	entry.response = response
	entry.recvtime = time.Now().Unix()

	if vfsCacheEntries >= vfsCacheEntriesTarget {
		select {
		case cleanNow <- struct{}{}:
		default:
		}
	}
}

func getFromCache(req *pb.ReadDirRequest, peer *connectivity.Peer) (found bool, age int, response *pb.ReadDirResponse, err error) {
	if !cacheIsEnabled() {
		return
	}

	cacheMtx.Lock()
	defer cacheMtx.Unlock()

	path := req.GetPath()
	if vfsCache[path] == nil || vfsCache[path][peer.Name] == nil {
		return
	}

	entry := vfsCache[path][peer.Name]
	found = true
	age = int(time.Now().Unix() - entry.recvtime)
	response = entry.response
	err = entry.err
	return
}

type cacheRef struct {
	path string
	peer string
	age  int
}

type cacheRefHeap []cacheRef

func (h cacheRefHeap) Len() int { return len(h) }
func (h cacheRefHeap) Less(i, j int) bool {
	// We want to find the highest age (oldest entry), so use > here
	return h[i].age > h[j].age
}
func (h cacheRefHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *cacheRefHeap) Push(x interface{}) {
	// Push and Pop use pointer receivers because they modify the slice's length,
	// not just its contents.
	*h = append(*h, x.(cacheRef))
}

func (h *cacheRefHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func aggressiveCacheClean(numToRemove int) {
	// mtx is held

	h := &cacheRefHeap{}
	for path, pathcache := range vfsCache {
		for peer, entry := range pathcache {
			age := int(time.Now().Unix() - entry.recvtime)
			heap.Push(h, cacheRef{
				path: path,
				peer: peer,
				age:  age,
			})
		}
	}

	for numToRemove > 0 {
		entry := heap.Pop(h).(cacheRef)
		dropCache(entry.path, entry.peer)
		numToRemove--
	}
}
