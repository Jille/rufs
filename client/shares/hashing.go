package shares

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

type hashListener func(circle, remoteFilename, hash string)

type callbackInfo struct {
	circle, remoteFilename, hash string
}

var (
	hashQueue    = make(chan hashRequest, 1000)
	hashCacheMtx sync.Mutex
	hashCache    = map[string]cachedHash{}
	listeners    []chan callbackInfo
)

type hashRequest struct {
	circle string
	local  string
	remote string
}

type cachedHash struct {
	hash  string
	mtime time.Time
	size  int64
}

func StartHash(circle, remoteFilename string) {
	local, err := resolveRemotePath(circle, remoteFilename)
	if err != nil {
		log.Printf("StartHash(%q, %q): resolveRemotePath(): %v", circle, remoteFilename, err)
		return
	}
	select {
	case hashQueue <- hashRequest{
		circle: circle,
		local:  local,
		remote: remoteFilename,
	}:
	default:
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
		hash, err := hashFile(req.local)
		if err != nil {
			log.Printf("Failed to hash %q: %v", req.local, err)
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

func getFileHashWithStat(fn string) (string, error) {
	var err error
	st, err := os.Stat(fn)
	if err != nil {
		return "", err
	}
	return getFileHash(fn, st), nil
}

func getFileHash(fn string, st os.FileInfo) string {
	hashCacheMtx.Lock()
	defer hashCacheMtx.Unlock()
	h, ok := hashCache[fn]
	if ok && h.mtime == st.ModTime() && h.size == st.Size() {
		return h.hash
	}
	if ok {
		delete(hashCache, fn)
	}
	return ""
}

func hashFile(fn string) (string, error) {
	fh, err := os.Open(fn)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return "", err
	}
	if h := getFileHash(fn, st); h != "" {
		// We already have the latest hash.
		return h, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", err
	}
	hash := fmt.Sprintf("%x", h.Sum(nil))
	hashCacheMtx.Lock()
	hashCache[fn] = cachedHash{
		hash:  hash,
		mtime: st.ModTime(),
		size:  st.Size(),
	}
	hashCacheMtx.Unlock()
	return hash, nil
}
