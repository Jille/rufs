package vfs

import (
	"context"
	"fmt"
	"log"
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
	FullPath    string
	IsDirectory bool
	Mtime       time.Time
	Size        uint64
}

func (f File) Basename() string {
	return filepath.Base(f.FullPath)
}

func (fs *VFS) Readdir(ctx context.Context, path string) (*Directory, error) {
	path = strings.Trim(path, "/")

	type peerFileInstance struct {
		peer        *connectivity.Peer
		isDirectory bool
	}
	type peerFile struct {
		instances []*peerFileInstance
	}

	warnings := []string{}
	files := make(map[string]*peerFile)

	for _, peer := range connectivity.AllPeers() {
		r, err := peer.ContentServiceClient().ReadDir(ctx, &pb.ReadDirRequest{
			Path: path,
		})
		if err != nil {
			log.Printf("failed to readdir on peer %s, ignoring: %v", peer.Name, err)
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
			peers := ""
			isDirectoryEverywhere := true
			for _, instance := range file.instances {
				if !instance.isDirectory {
					isDirectoryEverywhere = false
				}
				if peers == "" {
					peers = instance.peer.Name
				} else {
					peers = peers + ", " + instance.peer.Name
				}
			}
			if !isDirectoryEverywhere {
				warnings = append(warnings, fmt.Sprintf("File %s is available on multiple peers (%s), so it was hidden.", filename, peers))
				delete(files, filename)
			}
		}
	}

	res := &Directory{
		Files: map[string]*File{},
	}
	for filename, file := range files {
		res.Files[filename] = &File{
			FullPath:    path + "/" + filename,
			IsDirectory: file.instances[0].isDirectory,
			Mtime:       time.Time{},
			Size:        0,
		}
	}
	if len(warnings) >= 1 {
		log.Printf("warnings:")
		for _, warning := range warnings {
			log.Printf("- %s", warning)
		}
		res.Files["rufs-warnings.txt"] = &File{
			FullPath:    path + "/rufs-warnings.txt",
			IsDirectory: false,
			Mtime:       time.Time{},
			Size:        0,
		}
	}

	return res, nil
}
