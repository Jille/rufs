package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	filepath "path"
	"strings"
	"sync"
	"time"

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
	if end >= length {
		end = length
	}

	n := copy(buf, handle.content[offset:end])
	return n, nil
}

func (*fixedContentHandle) Close() error {
	return nil
}

func Open(ctx context.Context, path string) (Handle, error) {
	basename := filepath.Base(path)
	dirname := filepath.Dir(path)
	dir := Readdir(ctx, dirname)
	file := dir.Files[basename]
	if file == nil {
		log.Printf("ENOENT because {%s} did not exist in dir {%s}", basename, dirname)
		for fn := range dir.Files {
			log.Printf("File did exist: {%s}", fn)
		}
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
	t, err := transfers.GetTransferForFile(ctx, path, file.Hash, file.Size, file.Peers)
	if err != nil {
		log.Printf("GetTransferForFile %v", err)
		return nil, err
	}
	log.Printf("returns error %v", err)
	return t.GetHandle(), err
}

func Stat(ctx context.Context, path string) (*File, bool) {
	dn, fn := filepath.Split(path)
	ret := Readdir(ctx, dn)
	f, found := ret.Files[fn]
	if !found {
		return nil, false
	}
	return f, true
}

func Readdir(ctx context.Context, path string) *Directory {
	peers := connectivity.AllPeers()
	startTime := time.Now()
	go func() {
		circles := connectivity.CirclesFromPeers(peers)
		metrics.AddVfsReaddirs(circles, 1)
		metrics.AppendVfsReaddirLatency(circles, time.Since(startTime).Seconds())
	}()
	path = strings.Trim(path, "/")

	type peerFileInstance struct {
		peer *connectivity.Peer
		file *pb.File
	}
	type peerFile struct {
		instances []*peerFileInstance
	}

	var warnings []string
	files := make(map[string]*peerFile)

	resps, errs := parallelReadDir(ctx, peers, &pb.ReadDirRequest{
		Path: path,
	})
	for p, err := range errs {
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

	// remove files available on multiple peers, unless they are a directory everywhere
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
				triggerResolveConflict(ctx, filepath.Join(path, filename), peers)
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
			FullPath:    filepath.Join(path, filename),
			IsDirectory: file.instances[0].file.GetIsDirectory(),
			Mtime:       time.Unix(highestMtime, 0),
			Size:        file.instances[0].file.GetSize(),
			Hash:        file.instances[0].file.GetHash(),
			Peers:       peers,
		}
	}
	if len(warnings) >= 1 {
		warning := "*** RUFS encountered some issues showing this directory: ***\n"
		warning += strings.Join(warnings, "\n") + "\n"
		res.Files["rufs-warnings.txt"] = &File{
			FullPath:     path + "/rufs-warnings.txt",
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
			startTime := time.Now()
			r, err := p.ContentServiceClient().ReadDir(ctx, req)
			code := status.Code(err).String()
			metrics.AddVfsPeerReaddirs(circles, p.Name, code, 1)
			metrics.AppendVfsPeerReaddirLatency(circles, p.Name, code, time.Since(startTime).Seconds())
			mtx.Lock()
			defer mtx.Unlock()
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				// File not found on peer, don't include peer in errs or ret
			} else if err != nil {
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
