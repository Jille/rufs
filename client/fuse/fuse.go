// +build !windows,!darwin,!cgofuse

package fuse

import (
	"context"
	"flag"
	"fmt"
	"log"
	osUser "os/user"
	"strconv"
	"strings"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	billybazilfuse "github.com/Jille/billy-bazilfuse"
	"github.com/sgielen/rufs/client/config"
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
		fuse.MaxReadahead(1024 * 1024),
		fuse.AsyncRead(),
	}
	if !config.HasDirectIOPeers() {
		options = append(options, fuse.ReadOnly())
	}
	if len(f.allowedUsers) != 0 || *allowAllUsers {
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
	if err := fs.Serve(conn, billybazilfuse.New(vfs.GetFilesystem(), f.callHook)); err != nil {
		return err
	}
	<-conn.Ready
	return conn.MountError
}

func (f *Mount) callHook(ctx context.Context, req fuse.Request) error {
	if f.allowedUsers == nil {
		return nil
	}
	if !f.allowedUsers[req.Hdr().Uid] {
		return fuse.EPERM
	}
	return nil
}
