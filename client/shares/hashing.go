package shares

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"sync"
	"time"
)

type hashListener func(circle, remoteFilename, hash string)

type callbackInfo struct {
	circle, remoteFilename, hash string
}

var (
	hashCacheMtx     sync.Mutex
	hashQueue        = make(chan hashRequest, 1000)
	hashQueueEntries = map[string]struct{}{}
	hashCache        = map[string]cachedHash{}
	listeners        []chan callbackInfo
)

type hashRequest struct {
	circle        string
	localFilename string
	remote        string
}

type cachedHash struct {
	hash  string
	mtime time.Time
	size  int64
}

func StartHash(circle, remoteFilename string) {
	localFilename, err := resolveRemotePath(circle, remoteFilename)
	if err != nil {
		log.Printf("StartHash(%q, %q): resolveRemotePath(): %v", circle, remoteFilename, err)
		return
	}
	hashCacheMtx.Lock()
	defer hashCacheMtx.Unlock()
	hk := path.Join(circle, remoteFilename)
	if _, found := hashQueueEntries[hk]; found {
		return
	}
	hashQueueEntries[hk] = struct{}{}
	select {
	case hashQueue <- hashRequest{
		circle:        circle,
		localFilename: localFilename,
		remote:        remoteFilename,
	}:
	default:
		delete(hashQueueEntries, hk)
		log.Printf("StartHash(%q, %q): hash queue overflow", circle, remoteFilename)
	}
}

func RegisterHashListener(callback hashListener) {
	ch := make(chan callbackInfo, 1000)
	go func() {
		for ci := range ch {
			callback(ci.circle, ci.remoteFilename, ci.hash)
		}
	}()
	hashCacheMtx.Lock()
	listeners = append(listeners, ch)
	hashCacheMtx.Unlock()
}

func hashWorker() {
	for req := range hashQueue {
		hashCacheMtx.Lock()
		delete(hashQueueEntries, path.Join(req.circle, req.remote))
		hashCacheMtx.Unlock()
		hash, err := hashFile(req.localFilename)
		if err != nil {
			log.Printf("Failed to hash %q: %v", req.localFilename, err)
			continue
		}
		hashCacheMtx.Lock()
		li := listeners
		hashCacheMtx.Unlock()
		for _, l := range li {
			l <- callbackInfo{
				circle:         req.circle,
				remoteFilename: req.remote,
				hash:           hash,
			}
		}
	}
}

func getFileHashWithStat(localFilename string) (string, error) {
	var err error
	st, err := os.Stat(localFilename)
	if err != nil {
		return "", err
	}
	return getFileHash(localFilename, st), nil
}

func getFileHash(localFilename string, st os.FileInfo) string {
	hashCacheMtx.Lock()
	defer hashCacheMtx.Unlock()
	h, ok := hashCache[localFilename]
	if ok && h.mtime == st.ModTime() && h.size == st.Size() {
		return h.hash
	}
	if ok {
		delete(hashCache, localFilename)
	}
	return ""
}

func hashFile(localFilename string) (string, error) {
	fh, err := os.Open(localFilename)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return "", err
	}
	if h := getFileHash(localFilename, st); h != "" {
		// We already have the latest hash.
		return h, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", err
	}
	hash := fmt.Sprintf("%x", h.Sum(nil))
	hashCacheMtx.Lock()
	hashCache[localFilename] = cachedHash{
		hash:  hash,
		mtime: st.ModTime(),
		size:  st.Size(),
	}
	hashCacheMtx.Unlock()
	return hash, nil
}
