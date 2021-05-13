// +build !windows,!netbsd,!openbsd,!nofuse

package fuse

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	osUser "os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/sgielen/rufs/client/vfs"
)

var (
	enableFuseDebug = flag.Bool("enable_fuse_debug", false, "Enable fuse debug logging")
)

func NewMount(mountpoint string, allowUsers string) (*Mount, error) {
	var allowedUsers map[uint32]bool
	if allowUsers != "" {
		allowedUsers = map[uint32]bool{}
		for _, u := range strings.Split(allowUsers, ",") {
			pwd, err := osUser.Lookup(u)
			if err != nil {
				return nil, err
			}
			s, _ := strconv.ParseUint(pwd.Uid, 10, 32)
			allowedUsers[uint32(s)] = true
		}
	}

	res := &Mount{
		mountpoint:   mountpoint,
		allowedUsers: allowedUsers,
	}
	return res, nil
}

type Mount struct {
	mountpoint   string
	allowedUsers map[uint32]bool
}

func (f *Mount) Run(ctx context.Context) (retErr error) {
	if *enableFuseDebug {
		fuse.Debug = func(msg interface{}) { fmt.Println(msg) }
	}
	options := []fuse.MountOption{
		fuse.FSName("rufs"),
		fuse.Subtype("rufs"),
		fuse.VolumeName("rufs"),
		fuse.ReadOnly(),
		fuse.MaxReadahead(1024 * 1024),
	}
	if len(f.allowedUsers) != 0 {
		options = append(options, fuse.AllowOther())
	}
	conn, err := fuse.Mount(f.mountpoint, options...)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		select {
		case <-conn.Ready:
			if conn.MountError != nil {
				return
			}
			if err := fuse.Unmount(f.mountpoint); err != nil {
				log.Printf("Failed to unmount %q: %v", f.mountpoint, err)
			}
		case <-time.After(5 * time.Second):
			conn.Close()
		}
	}()
	fsDone := make(chan struct{})
	defer close(fsDone)
	defer func() {
		if err := conn.Close(); err != nil && retErr == nil {
			retErr = err
		}
	}()
	if err := fs.Serve(conn, f); err != nil {
		return err
	}
	<-conn.Ready
	return conn.MountError
}

func (fs *Mount) Root() (fs.Node, error) {
	return &dir{node{fs, ""}}, nil
}

type node struct {
	fs   *Mount
	path string
}

func (n *node) checkAccess(uid uint32) error {
	if n.fs.allowedUsers == nil {
		return nil
	}
	if !n.fs.allowedUsers[uid] {
		return fuse.EPERM
	}
	return nil
}

func (n *node) Access(ctx context.Context, req *fuse.AccessRequest) (retErr error) {
	return n.checkAccess(req.Header.Uid)
}

func (n *node) Attr(ctx context.Context, attr *fuse.Attr) (retErr error) {
	if n.path == "" {
		attr.Mode = 0755 | os.ModeDir
		return nil
	}
	dn, fn := filepath.Split(n.path)
	ret := vfs.Readdir(ctx, dn)
	if f, found := ret.Files[fn]; found {
		attr.Size = uint64(f.Size)
		if f.IsDirectory {
			attr.Mode = 0755 | os.ModeDir
		} else {
			attr.Mode = 0644
		}
		attr.Mtime = f.Mtime
		return nil
	}
	return fuse.ENOENT
}

func (n *node) Setattr(ctx context.Context, request *fuse.SetattrRequest, response *fuse.SetattrResponse) (retErr error) {
	return fuse.ENOSYS
}

func (n *node) Getxattr(ctx context.Context, req *fuse.GetxattrRequest, resp *fuse.GetxattrResponse) (retErr error) {
	return fuse.ENOSYS
}

func (n *node) Setxattr(ctx context.Context, req *fuse.SetxattrRequest) (retErr error) {
	return fuse.ENOSYS
}

type dir struct {
	node
}

func (d *dir) Create(ctx context.Context, request *fuse.CreateRequest, response *fuse.CreateResponse) (_ fs.Node, _ fs.Handle, retErr error) {
	return nil, nil, fuse.ENOSYS
}

func (d *dir) Lookup(ctx context.Context, name string) (_ fs.Node, retErr error) {
	path := filepath.Join(d.path, name)
	ret := vfs.Readdir(ctx, d.path)
	if f, found := ret.Files[name]; found {
		if f.IsDirectory {
			return &dir{node{d.fs, path}}, nil
		} else {
			return &file{node{d.fs, path}}, nil
		}
	}
	return nil, fuse.ENOENT
}

func (d *dir) Mkdir(ctx context.Context, request *fuse.MkdirRequest) (_ fs.Node, retErr error) {
	return nil, fuse.ENOSYS
}

func (d *dir) ReadDirAll(ctx context.Context) (_ []fuse.Dirent, retErr error) {
	ret := vfs.Readdir(ctx, d.path)

	dirents := make([]fuse.Dirent, 0, len(ret.Files))
	for fn, file := range ret.Files {
		var t fuse.DirentType
		if file.IsDirectory {
			t = fuse.DT_Dir
		} else {
			t = fuse.DT_File
		}
		dirents = append(dirents, fuse.Dirent{
			Name: fn,
			Type: t,
		})
	}
	return dirents, nil
}

func (d *dir) Remove(ctx context.Context, request *fuse.RemoveRequest) error {
	return fuse.ENOSYS
}

type file struct {
	node
}

func (f *file) Open(ctx context.Context, request *fuse.OpenRequest, response *fuse.OpenResponse) (_ fs.Handle, retErr error) {
	if err := f.checkAccess(request.Header.Uid); err != nil {
		return nil, err
	}
	ret, err := vfs.Open(ctx, f.path)
	if err != nil {
		if err.Error() == "ENOENT" {
			return nil, fuse.ENOENT
		}
		return nil, err
	}
	return &handle{f.node, ret}, nil
}

type handle struct {
	node
	vh vfs.Handle
}

func (h *handle) Read(ctx context.Context, request *fuse.ReadRequest, response *fuse.ReadResponse) (retErr error) {
	response.Data, retErr = h.vh.Read(ctx, request.Offset, int64(request.Size))
	if retErr != nil {
		log.Printf("VFS read failed for {%s}: %v", h.path, retErr)
	}
	return retErr
}

func (h *handle) Write(ctx context.Context, request *fuse.WriteRequest, response *fuse.WriteResponse) (retErr error) {
	return fuse.ENOSYS
}

func (h *handle) Fsync(ctx context.Context, request *fuse.FsyncRequest) error {
	return fuse.ENOSYS
}

func (h *handle) Release(ctx context.Context, request *fuse.ReleaseRequest) error {
	h.vh.Close()
	return nil
}
