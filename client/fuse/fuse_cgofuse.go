//go:build windows || darwin || cgofuse

package fuse

import (
	"context"
	"errors"
	"flag"
	osUser "os/user"
	"runtime"
	"strconv"
	"strings"
	"sync"

	billycgofuse "github.com/Jille/billy-cgofuse"
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
		mountpoint:   strings.TrimRight(mountpoint, `/\`),
		allowedUsers: allowedUsers,
	}
	return res, nil
}

type Mount struct {
	fuse.FileSystemBase
	mtx          sync.Mutex
	mountpoint   string
	allowedUsers map[uint32]bool
}

func (f *Mount) Run(ctx context.Context) (retErr error) {
	host := fuse.NewFileSystemHost(billycgofuse.New(vfs.GetFilesystem()))
	host.SetCapCaseInsensitive(false)
	host.SetCapReaddirPlus(true)
	// TODO: small readahead, allow others if len(f.allowedUsers) != 0
	options := []string{"-o", "ro", "-o", "uid=-1", "-o", "gid=-1"}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		options = append(options, "-o", "volname=RUFS")
	}
	if *enableFuseDebug {
		options = append(options, "-o", "debug")
	}
	go func() {
		<-ctx.Done()
		host.Unmount()
	}()
	if !host.Mount(f.mountpoint, options) {
		return errors.New("failed to initialize cgofuse mount")
	}
	return nil
}
