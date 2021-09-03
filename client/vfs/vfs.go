package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	// Paths within the VFS layer are always separated by forward slahes, e.g. "foo/bar", even on
	// Windows. Calls into the VFS layer are assumed to follow this standard.
	"path"

	"github.com/Jille/billy-router/emptyfs"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/helper/polyfill"
	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/client/metrics"
	"github.com/sgielen/rufs/client/transfers"
	"github.com/sgielen/rufs/client/vfs/readonlyhandle"
	"github.com/sgielen/rufs/common"
	pb "github.com/sgielen/rufs/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GetFilesystem() billy.Filesystem {
	return polyfill.New(mergeFS{})
}

type mergeFS struct{}

var _ billy.Basic = mergeFS{}
var _ billy.Dir = mergeFS{}

type Directory struct {
	Files map[string]*File
}

type File struct {
	fullPath     string
	isDirectory  bool
	mtime        time.Time
	size         int64
	hash         string
	peers        []*connectivity.Peer
	fixedContent []byte
}

var _ os.FileInfo = &File{}

func (f *File) Name() string {
	return path.Base(f.fullPath)
}

func (f *File) IsDir() bool {
	return f.isDirectory
}

func (f *File) Mode() os.FileMode {
	if f.isDirectory {
		return 0555 | os.ModeDir
	}
	return 0444
}

func (f *File) ModTime() time.Time {
	return f.mtime
}

func (f *File) Size() int64 {
	return f.size
}

func (f *File) Sys() interface{} {
	return nil
}

type fixedContentHandle struct {
	content []byte
}

func (handle *fixedContentHandle) ReadAt(buf []byte, offset int64) (int, error) {
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

func (mergeFS) Open(p string) (billy.File, error) {
	ctx := context.Background()
	basename := path.Base(p)
	dirname := path.Dir(p)
	dir := readdirImpl(ctx, dirname, true)
	file := dir.Files[basename]
	if file == nil {
		return nil, errors.New("ENOENT")
	}
	if file.isDirectory {
		return nil, errors.New("EISDIR")
	}
	if len(file.fixedContent) > 0 {
		metrics.AddVfsFixedContentOpens(connectivity.CirclesFromPeers(connectivity.AllPeers()), basename, 1)
		return readonlyhandle.New(&fixedContentHandle{
			content: file.fixedContent,
		}, p), nil
	}
	t, err := transfers.GetTransferForFile(ctx, p, file.hash, file.size, file.peers)
	if err != nil {
		return nil, err
	}
	return readonlyhandle.New(t.GetHandle(), p), nil
}

func (mergeFS) Stat(p string) (os.FileInfo, error) {
	if p == "" || p == "/" || p == "." {
		return emptyfs.New().Stat(p)
	}
	dn, fn := path.Split(p)
	ret := readdirImpl(context.Background(), dn, true)
	f, found := ret.Files[fn]
	if !found {
		return nil, os.ErrNotExist
	}
	return f, nil
}

func (mergeFS) ReadDir(p string) ([]os.FileInfo, error) {
	d := Readdir(context.Background(), p)
	ret := make([]os.FileInfo, 0, len(d.Files))
	for _, f := range d.Files {
		ret = append(ret, f)
	}
	return ret, nil
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
			fullPath:    path.Join(p, filename),
			isDirectory: file.instances[0].file.GetIsDirectory(),
			mtime:       time.Unix(highestMtime, 0),
			size:        file.instances[0].file.GetSize(),
			hash:        file.instances[0].file.GetHash(),
			peers:       peers,
		}
	}
	if len(warnings) >= 1 {
		warning := "*** RUFS encountered some issues showing this directory: ***\n"
		sort.Strings(warnings)
		warning += strings.Join(warnings, "\n") + "\n"
		res.Files["rufs-warnings.txt"] = &File{
			fullPath:     p + "/rufs-warnings.txt",
			isDirectory:  false,
			mtime:        time.Now(),
			size:         int64(len(warning)),
			fixedContent: []byte(warning),
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

func (mergeFS) Create(string) (billy.File, error) {
	return nil, os.ErrPermission
}

func (m mergeFS) OpenFile(fn string, flag int, perm os.FileMode) (billy.File, error) {
	if flag&(os.O_RDWR|os.O_WRONLY|os.O_CREATE) > 0 {
		return nil, os.ErrPermission
	}
	return m.Open(fn)
}

func (mergeFS) Join(elem ...string) string {
	return path.Join(elem...)
}

func (mergeFS) Remove(string) error {
	return os.ErrPermission
}

func (mergeFS) Rename(string, string) error {
	return os.ErrPermission
}

func (mergeFS) MkdirAll(string, os.FileMode) error {
	return os.ErrPermission
}
