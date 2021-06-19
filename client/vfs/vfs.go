package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	// Paths within the VFS layer are always separated by forward slahes, e.g. "foo/bar", even on
	// Windows. Calls into the VFS layer are assumed to follow this standard.
	"path"

	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/client/metrics"
	"github.com/sgielen/rufs/client/transfers"
	"github.com/sgielen/rufs/common"
	pb "github.com/sgielen/rufs/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Directory struct {
	Files map[string]*File
}

type File struct {
	FullPath     string
	IsDirectory  bool
	Mtime        time.Time
	Size         int64
	Hash         string
	Peers        []*connectivity.Peer
	FixedContent []byte
}

type Handle interface {
	Read(ctx context.Context, offset int64, buf []byte) (int, error)
	Close() error
}

type fixedContentHandle struct {
	content []byte
}

func (handle *fixedContentHandle) Read(ctx context.Context, offset int64, buf []byte) (int, error) {
	length := int64(len(handle.content))
	if offset >= length {
		return 0, io.EOF
	}
	end := offset + int64(len(buf))
	if end > length {
		end = length
	}
	n := copy(buf, handle.content[offset:end])
	return n, nil
}

func (*fixedContentHandle) Close() error {
	return nil
}

func Open(ctx context.Context, p string) (Handle, error) {
	basename := path.Base(p)
	dirname := path.Dir(p)
	dir := readdirImpl(ctx, dirname, true)
	file := dir.Files[basename]
	if file == nil {
		return nil, errors.New("ENOENT")
	}
	if file.IsDirectory {
		return nil, errors.New("EISDIR")
	}
	if len(file.FixedContent) > 0 {
		metrics.AddVfsFixedContentOpens(connectivity.CirclesFromPeers(connectivity.AllPeers()), basename, 1)
		return &fixedContentHandle{
			content: file.FixedContent,
		}, nil
	}
	t, err := transfers.GetTransferForFile(ctx, p, file.Hash, file.Size, file.Peers)
	if err != nil {
		return nil, err
	}
	return t.GetHandle(), err
}

func Stat(ctx context.Context, p string) (*File, bool) {
	dn, fn := path.Split(p)
	ret := readdirImpl(ctx, dn, true)
	f, found := ret.Files[fn]
	if !found {
		return nil, false
	}
	return f, true
}

func Readdir(ctx context.Context, p string) *Directory {
	startTime := time.Now()
	go func() {
		peers := connectivity.AllPeers()
		circles := connectivity.CirclesFromPeers(peers)
		metrics.AddVfsReaddirs(circles, 1)
		metrics.AppendVfsReaddirLatency(circles, time.Since(startTime).Seconds())
	}()

	return readdirImpl(ctx, p, false)
}

func readdirImpl(ctx context.Context, p string, preferCache bool) *Directory {
	p = strings.Trim(p, "/")

	type peerFileInstance struct {
		peer *connectivity.Peer
		file *pb.File
	}
	type peerFile struct {
		instances []*peerFileInstance
	}

	var warnings []string
	files := make(map[string]*peerFile)

	allPeers := connectivity.AllPeers()
	resps := map[*connectivity.Peer]*pb.ReadDirResponse{}
	errs := map[*connectivity.Peer]error{}

	req := &pb.ReadDirRequest{
		Path: p,
	}

	var fanoutPeers []*connectivity.Peer
	if preferCache && cacheIsEnabled() {
		for _, peer := range allPeers {
			found, age, resp, err := getFromCache(req, peer)
			if !found || age > cacheAgeRecent || (resp == nil && err == nil) {
				fanoutPeers = append(fanoutPeers, peer)
				continue
			}
			if resp != nil {
				resps[peer] = resp
			}
			if err != nil {
				errs[peer] = err
			}
		}
	} else {
		fanoutPeers = allPeers
	}

	if len(fanoutPeers) > 0 {
		respsP, errsP := parallelReadDir(ctx, fanoutPeers, req)
		for p, resp := range respsP {
			resps[p] = resp
		}
		for p, err := range errsP {
			errs[p] = err
		}
	}

	for p, err := range errs {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			// File not found on peer, don't include this error in warnings
			continue
		}

		warnings = append(warnings, fmt.Sprintf("failed to readdir on peer %s, ignoring: %v", p.Name, err))
	}
	for p, r := range resps {
		for _, file := range r.Files {
			if files[file.Filename] == nil {
				files[file.Filename] = &peerFile{}
			}
			instance := &peerFileInstance{
				peer: p,
				file: file,
			}
			files[file.Filename].instances = append(files[file.Filename].instances, instance)
		}
	}

	// remove files available on multiple peers, unless they are a directory everywhere or the hash is equal everywhere
	for filename, file := range files {
		if len(file.instances) != 1 {
			var peers []string
			hashes := map[string]bool{}
			isDirectoryEverywhere := true
			for _, instance := range file.instances {
				if !instance.file.GetIsDirectory() {
					isDirectoryEverywhere = false
					hashes[instance.file.GetHash()] = true
				}
				peers = append(peers, instance.peer.Name)
			}
			if !isDirectoryEverywhere && (len(hashes) != 1 || hashes[""]) {
				warnings = append(warnings, fmt.Sprintf("File %s is available on multiple peers (%s), so it was hidden.", filename, strings.Join(peers, ", ")))
				delete(files, filename)
				triggerResolveConflict(ctx, path.Join(p, filename), peers)
			}
		}
	}

	res := &Directory{
		Files: map[string]*File{},
	}
	for filename, file := range files {
		peers := []*connectivity.Peer{}
		var highestMtime int64
		for _, instance := range file.instances {
			peers = append(peers, instance.peer)
			if instance.file.GetMtime() > highestMtime {
				highestMtime = instance.file.GetMtime()
			}
		}
		res.Files[filename] = &File{
			FullPath:    path.Join(p, filename),
			IsDirectory: file.instances[0].file.GetIsDirectory(),
			Mtime:       time.Unix(highestMtime, 0),
			Size:        file.instances[0].file.GetSize(),
			Hash:        file.instances[0].file.GetHash(),
			Peers:       peers,
		}
	}
	if len(warnings) >= 1 {
		warning := "*** RUFS encountered some issues showing this directory: ***\n"
		sort.Strings(warnings)
		warning += strings.Join(warnings, "\n") + "\n"
		res.Files["rufs-warnings.txt"] = &File{
			FullPath:     p + "/rufs-warnings.txt",
			IsDirectory:  false,
			Mtime:        time.Now(),
			Size:         int64(len(warning)),
			FixedContent: []byte(warning),
		}
	}

	return res
}

func parallelReadDir(ctx context.Context, peers []*connectivity.Peer, req *pb.ReadDirRequest) (map[*connectivity.Peer]*pb.ReadDirResponse, map[*connectivity.Peer]error) {
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	var mtx sync.Mutex
	ret := map[*connectivity.Peer]*pb.ReadDirResponse{}
	errs := map[*connectivity.Peer]error{}
	var wg sync.WaitGroup
	wg.Add(len(peers))
	for _, p := range peers {
		p := p
		go func() {
			defer wg.Done()
			circles := []string{common.CircleFromPeer(p.Name)}
			type res struct {
				r   *pb.ReadDirResponse
				err error
			}
			ch := make(chan res, 1)
			go func() {
				startTime := time.Now()
				readDirCtx, readDirCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer readDirCancel()
				r, err := p.ContentServiceClient().ReadDir(readDirCtx, req)
				ch <- res{r, err}
				// Store response in cache (even if it's transient, so we don't retry on stat/open)
				putCache(req, p, r, err)
				code := status.Code(err).String()
				metrics.AddVfsPeerReaddirs(circles, p.Name, code, 1)
				metrics.AppendVfsPeerReaddirLatency(circles, p.Name, code, time.Since(startTime).Seconds())
			}()
			var r *pb.ReadDirResponse
			var err error
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case res := <-ch:
				r = res.r
				err = res.err
			}
			if status.Code(err) == codes.DeadlineExceeded || err == context.DeadlineExceeded {
				// Retrieve response from cache if possible
				found, age, lastResponse, lastErr := getFromCache(req, p)
				if found && age < cacheAgeExpired {
					r = lastResponse
					err = lastErr
				} else {
					// Cache deadline exceeded too, so we don't retry on stat/open
					putCache(req, p, nil, err)
				}
			}
			mtx.Lock()
			defer mtx.Unlock()
			if err != nil {
				errs[p] = err
			} else {
				ret[p] = r
			}
		}()
	}
	wg.Wait()
	return ret, errs
}

func triggerResolveConflict(ctx context.Context, filename string, peers []string) {
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	circles := common.CirclesFromPeers(peers)
	for _, c := range circles {
		if _, err := connectivity.DiscoveryClient(c).ResolveConflict(ctx, &pb.ResolveConflictRequest{
			Filename: filename,
		}); err != nil {
			log.Printf("Failed to start conflict resolution for %q: %v", filename, err)
		}
	}
}
