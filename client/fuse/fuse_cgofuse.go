// +build windows darwin cgofuse

package fuse

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	osUser "os/user"
	"strconv"
	"strings"
	"sync"

	"github.com/billziss-gh/cgofuse/fuse"
	"github.com/sgielen/rufs/client/vfs"
)

var (
	allowAllUsers   = flag.Bool("allow_all_users", false, "Allow all users access to the FUSE mount")
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
		handles:      map[uint64]vfs.Handle{},
	}
	return res, nil
}

type Mount struct {
	fuse.FileSystemBase
	mtx          sync.Mutex
	mountpoint   string
	allowedUsers map[uint32]bool
	handles      map[uint64]vfs.Handle
	lastHandle   uint64
}

func (f *Mount) Run(ctx context.Context) (retErr error) {
	host := fuse.NewFileSystemHost(f)
	host.SetCapCaseInsensitive(false)
	host.SetCapReaddirPlus(true)
	// TODO: small readahead, allow others if len(f.allowedUsers) != 0
	options := []string{"-o", "ro", "-o", "volname=RUFS", "-o", "uid=-1", "-o", "gid=-1"}
	if *enableFuseDebug {
		options = append(options, "-o", "debug")
	}
	if !host.Mount(f.mountpoint, options) {
		return errors.New("failed to initialize cgofuse mount")
	}
	go func() {
		<-ctx.Done()
		host.Unmount()
	}()
	return nil
}

func (f *Mount) checkAccess() int {
	if f.allowedUsers == nil {
		return 0
	}
	uid, _, _ := fuse.Getcontext()
	if f.allowedUsers[uid] {
		return -fuse.EPERM
	}
	return 0
}

func stripPath(path string) string {
	return strings.TrimPrefix(path, "/")
}

func (f *Mount) Access(_ string, mask uint32) int {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	return f.checkAccess()
}

func (f *Mount) Getattr(path string, attr *fuse.Stat_t, fh uint64) int {
	path = stripPath(path)
	if path == "" || path == "/" {
		attr.Mode = 0555 | fuse.S_IFDIR
		return 0
	}
	file, found := vfs.Stat(context.Background(), path)
	if !found {
		return -fuse.ENOENT
	}
	attr.Size = file.Size
	if file.IsDirectory {
		attr.Mode = 0755 | fuse.S_IFDIR
	} else {
		attr.Mode = 0644 | fuse.S_IFREG
	}
	attr.Mtim.Sec = file.Mtime.Unix()
	attr.Mtim.Nsec = file.Mtime.UnixNano()
	return 0
}

func (f *Mount) Readdir(path string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	path = stripPath(path)
	f.mtx.Lock()
	defer f.mtx.Unlock()
	ret := vfs.Readdir(context.Background(), path)

	for fn, f := range ret.Files {
		attr := fuse.Stat_t{}
		attr.Size = f.Size
		if f.IsDirectory {
			attr.Mode = 0755 | fuse.S_IFDIR
		} else {
			attr.Mode = 0644
		}
		attr.Mtim.Sec = f.Mtime.Unix()
		attr.Mtim.Nsec = f.Mtime.UnixNano()
		if !fill(fn, &attr, 0) {
			break
		}
	}
	return 0
}

func (f *Mount) Open(path string, flags int) (int, uint64) {
	path = stripPath(path)
	f.mtx.Lock()
	defer f.mtx.Unlock()
	if access := f.checkAccess(); access != 0 {
		return access, 0
	}
	handle, err := vfs.Open(context.Background(), path)
	if err != nil {
		if err.Error() == "ENOENT" {
			return -fuse.ENOENT, 0
		}
		return -fuse.EIO, 0
	}
	f.lastHandle += 1
	fh := f.lastHandle
	f.handles[fh] = handle
	return 0, fh
}

func (f *Mount) Read(path string, buff []byte, offset int64, fh uint64) int {
	path = stripPath(path)
	f.mtx.Lock()
	handle, ok := f.handles[fh]
	f.mtx.Unlock()
	if !ok {
		return -fuse.EIO
	}
	n, err := handle.Read(context.Background(), offset, buff)
	if err != nil && err != io.EOF {
		log.Printf("VFS read failed for {%s}: %v", path, err)
		return -fuse.EIO
	}
	return n
}

func (f *Mount) Release(_ string, fh uint64) int {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	handle, ok := f.handles[fh]
	if !ok {
		return -fuse.EIO
	}
	handle.Close()
	delete(f.handles, fh)
	return 0
}
