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
	"time"

	"github.com/sgielen/rufs/client/connectivity"
	pb "github.com/sgielen/rufs/proto"
)

var (
	vfs VFS
)

func GetVFS() VFS {
	return vfs
}

type VFS struct {
}

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

func (fs VFS) Open(ctx context.Context, path string) (*Handle, error) {
	basename := filepath.Base(path)
	dirname := filepath.Dir(path)
	dir, err := fs.Readdir(ctx, dirname)
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

func (fs VFS) Readdir(ctx context.Context, path string) (*Directory, error) {
	path = strings.Trim(path, "/")

	type peerFileInstance struct {
		peer        *connectivity.Peer
		isDirectory bool
	}
	type peerFile struct {
		instances []*peerFileInstance
	}

	var warnings []string
	files := make(map[string]*peerFile)

	for _, peer := range connectivity.AllPeers() {
		r, err := peer.ContentServiceClient().ReadDir(ctx, &pb.ReadDirRequest{
			Path: path,
		})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to readdir on peer %s, ignoring: %v", peer.Name, err))
			continue
		}
		for _, file := range r.Files {
			if files[file.Filename] == nil {
				files[file.Filename] = &peerFile{}
			}
			instance := &peerFileInstance{
				peer:        peer,
				isDirectory: file.GetIsDirectory(),
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
				if !instance.isDirectory {
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
		for _, instance := range file.instances {
			peers = append(peers, instance.peer)
		}
		res.Files[filename] = &File{
			FullPath:    filepath.Join(path, filename),
			IsDirectory: file.instances[0].isDirectory,
			Mtime:       time.Time{},
			Size:        0,
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
