package shares

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/config"
	pb "github.com/sgielen/rufs/proto"
	"github.com/yookoala/realpath"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	circles map[string]*circle
)

type circle struct {
	shares map[string]string
}

func Init() error {
	circles = map[string]*circle{}
	for _, cfg := range config.GetCircles() {
		c := &circle{
			shares: map[string]string{},
		}
		for _, s := range cfg.Shares {
			local, err := resolveSharePath(s)
			if err != nil {
				return fmt.Errorf("invalid share %q: %v", s.Remote, err)
			}
			c.shares[s.Remote] = local
		}
		circles[cfg.Name] = c
	}
	go hashWorker()
	connectivity.HandleResolveConflictRequest = handleResolveConflictRequest
	return nil
}

func resolveSharePath(s *config.Share) (string, error) {
	if strings.Contains(s.Remote, "/") || s.Remote == "." || s.Remote == ".." {
		return "", fmt.Errorf("remote path invalid: %s", s.Remote)
	}

	local, err := realpath.Realpath(s.Local)
	if err != nil {
		return "", fmt.Errorf("local path {%s} resolve failed: %v", s.Local, err)
	}
	lstat, err := os.Lstat(local)
	if err != nil {
		return "", fmt.Errorf("local path {%s} stat failed: %v", s.Local, err)
	}
	if !lstat.IsDir() {
		return "", fmt.Errorf("local path {%s} is not a directory", s.Local)
	}
	return local, nil
}

func resolveRemotePath(circle, remotePath string) (string, error) {
	c := circles[circle]
	sp := strings.SplitN(remotePath, "/", 2)
	remote := sp[0]
	remainder := ""
	if len(sp) == 2 {
		remainder = sp[1]
	}
	shareRoot, ok := c.shares[remote]
	if !ok {
		return "", status.Errorf(codes.NotFound, "share %s not found", remote)
	}
	if strings.Contains(remainder, "../") {
		return "", status.Error(codes.InvalidArgument, "illegal path containing ../ refused")
	}
	localPath := filepath.Join(shareRoot, remainder)
	realLocalPath, err := realpath.Realpath(localPath)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(strings.TrimSuffix(realLocalPath, "/"), shareRoot) {
		return "", status.Errorf(codes.InvalidArgument, "share %s not found", remote)
	}
	return localPath, nil
}

func Open(circle, remotePath string) (*os.File, error) {
	localPath, err := resolveRemotePath(circle, remotePath)
	if err != nil {
		return nil, err
	}
	fh, err := os.Open(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "file %q not found", remotePath)
		}
		return nil, status.Errorf(codes.ResourceExhausted, "failed to open %q: %v", remotePath, err)
	}
	return fh, nil
}

func Stat(circle, remotePath string) (os.FileInfo, error) {
	localPath, err := resolveRemotePath(circle, remotePath)
	if err != nil {
		return nil, err
	}
	return os.Stat(localPath)
}

func Readdir(circle, remotePath string) ([]*pb.File, error) {
	var ret []*pb.File
	if remotePath == "" {
		for remote := range circles[circle].shares {
			ret = append(ret, &pb.File{
				Filename:    remote,
				IsDirectory: true,
				Mtime:       time.Now().Unix(),
			})
		}
		return ret, nil
	}
	dh, err := Open(circle, remotePath)
	if err != nil {
		return nil, err
	}
	defer dh.Close()
	entries, err := dh.Readdir(0)
	if err != nil {
		return nil, err
	}
	for _, dirfile := range entries {
		file := &pb.File{
			Filename:    dirfile.Name(),
			IsDirectory: dirfile.IsDir(),
			Size:        dirfile.Size(),
			Mtime:       dirfile.ModTime().Unix(),
		}
		if h := getFileHash(filepath.Join(dh.Name(), dirfile.Name()), dirfile); h != "" {
			file.Hash = h
		}
		ret = append(ret, file)
	}
	return ret, nil
}
