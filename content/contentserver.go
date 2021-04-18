package content

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/sgielen/rufs/config"
	pb "github.com/sgielen/rufs/proto"
	"github.com/yookoala/realpath"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/reflection"
)

func New(addr string, configuration *config.Config) (*content, error) {
	if addr == "" {
		return nil, errors.New("missing parameter addr")
	}

	for _, circle := range configuration.Circles {
		for _, share := range circle.Shares {
			if strings.Contains(share.Remote, "/") || share.Remote == "." || share.Remote == ".." {
				return nil, fmt.Errorf("remote path invalid: %s", share.Remote)
			}

			local, err := realpath.Realpath(share.Local)
			if err != nil {
				return nil, fmt.Errorf("local path {%s} resolve failed: %v", share.Local, err)
			}
			lstat, err := os.Lstat(local)
			if err != nil {
				return nil, fmt.Errorf("local path {%s} stat failed: %v", share.Local, err)
			}
			if !lstat.IsDir() {
				return nil, fmt.Errorf("local path {%s} is not a directory", share.Local)
			}

			share.Local = local
		}
	}

	c := &content{
		addr:          addr,
		configuration: configuration,
	}
	return c, nil
}

type content struct {
	pb.UnimplementedContentServiceServer

	addr          string
	configuration *config.Config
}

func (c *content) Run() {
	s := grpc.NewServer()
	pb.RegisterContentServiceServer(s, c)
	reflection.Register(s)
	sock, err := net.Listen("tcp", c.addr)
	if err != nil {
		log.Fatalf("content server failed to listen on %s: %v", c.addr, err)
	}
	log.Printf("content server listening on addr %s.", c.addr)
	if err := s.Serve(sock); err != nil {
		log.Fatalf("content server failed to serve on %s: %v", c.addr, err)
	}
}

func (c *content) getLocalPath(shares []*config.Share, path string) (string, error) {
	// find matching share
	remote := strings.Split(path, "/")[0]
	log.Printf("remote={%s}", remote)
	var matchingShare *config.Share
	for _, share := range shares {
		if share.Remote == remote {
			matchingShare = share
			break
		}
	}

	if matchingShare == nil {
		return "", status.Errorf(codes.NotFound, "share %s not found", remote)
	}

	path = path[len(remote):]
	for len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	root := matchingShare.Local
	log.Printf("matching root={%s}, path={%s}", root, path)
	dirpath := filepath.Clean(root + path)
	if dirpath != root && !strings.HasPrefix(dirpath, root+"/") {
		return "", status.Errorf(codes.PermissionDenied, "path falls outside root")
	}

	// check if realpath also falls inside root
	dirpath, err := realpath.Realpath(root + path)
	if err != nil {
		// try not to return the original path
		if pe, ok := err.(*os.PathError); ok {
			return "", pe.Unwrap()
		} else {
			return "", err
		}
	}
	if dirpath != root && !strings.HasPrefix(dirpath, root+"/") {
		return "", status.Errorf(codes.PermissionDenied, "path falls outside root")
	}
	return dirpath, nil
}

func (c *content) ReadDir(ctx context.Context, req *pb.ReadDirRequest) (*pb.ReadDirResponse, error) {
	reqpath := strings.TrimLeft(req.GetPath(), "/")

	res := &pb.ReadDirResponse{
		Files: []*pb.File{},
	}

	// TODO(sjors): take circle name from connection context
	shares := c.configuration.Circles[0].Shares

	if reqpath == "" {
		for _, share := range shares {
			file := &pb.File{
				Filename:    share.Remote,
				Hash:        "",
				IsDirectory: true,
			}
			res.Files = append(res.Files, file)
		}
		return res, nil
	}

	log.Printf("get local path for reqpath {%s}", reqpath)
	dirpath, err := c.getLocalPath(shares, reqpath)
	if err != nil {
		return nil, err
	}
	dirfiles, err := readdir(dirpath)
	if err != nil {
		return nil, err
	}
	for _, dirfile := range dirfiles {
		file := &pb.File{
			Filename:    dirfile.Name(),
			Hash:        "",
			IsDirectory: dirfile.IsDir(),
		}
		res.Files = append(res.Files, file)
	}
	return res, nil
}

// Compatibility function to support Go 1.13.
func readdir(name string) ([]os.FileInfo, error) {
	dh, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer dh.Close()
	return dh.Readdir(0)
}

func (c *content) ReadFile(req *pb.ReadFileRequest, stream pb.ContentService_ReadFileServer) error {
	reqpath := strings.TrimLeft(req.GetFilename(), "/")

	// TODO(sjors): take circle name from connection context
	shares := c.configuration.Circles[0].Shares

	if reqpath == "" {
		return status.Errorf(codes.FailedPrecondition, "is a directory")
	}

	path, err := c.getLocalPath(shares, reqpath)
	if err != nil {
		return err
	}
	fh, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status.Errorf(codes.NotFound, "file %q not found", req.GetFilename())
		}
		return status.Errorf(codes.FailedPrecondition, "failed to open %q: %v", req.GetFilename(), err)
	}
	defer fh.Close()
	var buf [8192]byte
	offset := req.GetOffset()
	remaining := req.GetRdnow()
	readNowDone := false
	for {
		for remaining <= 0 {
			if readNowDone {
				return nil
			}
			remaining = req.GetRdahead()
			readNowDone = true
		}
		r := remaining
		if r > uint64(len(buf)) {
			r = uint64(len(buf))
		}
		sn, err := fh.ReadAt(buf[:r], int64(offset))
		if err != nil && err != io.EOF {
			return status.Errorf(codes.ResourceExhausted, "failed to read from %q at %d: %v", req.GetFilename(), offset, err)
		}
		n := uint64(sn)
		if err := stream.Send(&pb.ReadFileResponse{
			Offset: offset,
			Data:   buf[:n],
		}); err != nil {
			return err
		}
		if n < r {
			// Short read, so we hit EOF.
			return nil
		}
		offset += n
		remaining -= n
	}
}
