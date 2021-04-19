package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sgielen/rufs/client/connectivity"
	pb "github.com/sgielen/rufs/proto"
)

type Directory struct {
	Files map[string]*File
}

type File struct {
	FullPath     string
	IsDirectory  bool
	Mtime        time.Time
	Size         uint64
	Peers        []*connectivity.Peer
	FixedContent []byte
}

type Handle struct {
	Peer         *connectivity.Peer
	FullPath     string
	FixedContent []byte
}

func (handle *Handle) Read(ctx context.Context, offset uint64, size uint64) ([]byte, error) {
	if handle.Peer == nil {
		length := uint64(len(handle.FixedContent))
		if offset >= length {
			return nil, io.EOF
		}
		if offset+size >= length {
			size = length - offset
		}
		return handle.FixedContent[offset : offset+size], nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := handle.Peer.ContentServiceClient().ReadFile(ctx, &pb.ReadFileRequest{
		Filename: handle.FullPath,
		Offset:   offset,
		Rdnow:    size,
		Rdahead:  0,
	})
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0, size)
	for len(buf) < int(size) {
		res, err := stream.Recv()
		if err != nil {
			log.Printf("Recv failed: %v", err)
			return nil, err
		}
		buf = append(buf, res.Data...)
	}
	return buf, nil
}

func Open(ctx context.Context, path string) (*Handle, error) {
	basename := filepath.Base(path)
	dirname := filepath.Dir(path)
	dir, err := Readdir(ctx, dirname)
	if err != nil {
		return nil, err
	}
	file := dir.Files[basename]
	if file == nil {
		return nil, errors.New("ENOENT")
	}
	if file.IsDirectory {
		return nil, errors.New("EISDIR")
	}
	var peer *connectivity.Peer
	if len(file.Peers) >= 1 {
		// TODO(sjors): do something smarter than selecting a random peer.
		peer = file.Peers[rand.Intn(len(file.Peers))]
	}
	return &Handle{
		Peer:         peer,
		FullPath:     path,
		FixedContent: file.FixedContent,
	}, nil
}

func Readdir(ctx context.Context, path string) (*Directory, error) {
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

	resps, errs := parallelReadDir(ctx, connectivity.AllPeers(), &pb.ReadDirRequest{
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
			isDirectoryEverywhere := true
			for _, instance := range file.instances {
				if !instance.file.GetIsDirectory() {
					isDirectoryEverywhere = false
				}
				peers = append(peers, instance.peer.Name)
			}
			if !isDirectoryEverywhere {
				warnings = append(warnings, fmt.Sprintf("File %s is available on multiple peers (%s), so it was hidden.", filename, strings.Join(peers, ", ")))
				delete(files, filename)
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
			Size:         uint64(len(warning)),
			FixedContent: []byte(warning),
		}
	}

	return res, nil
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
			r, err := p.ContentServiceClient().ReadDir(ctx, req)
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
