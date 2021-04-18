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

	pb "github.com/sgielen/rufs/proto"
	"github.com/yookoala/realpath"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/reflection"
)

func New(addr string, root string) (*content, error) {
	if addr == "" || root == "" {
		return nil, errors.New("missing parameter addr or root")
	}

	rpath, err := realpath.Realpath(root)
	if err != nil {
		return nil, fmt.Errorf("content server root invalid: %v", err)
	}

	c := &content{
		addr: addr,
		root: rpath,
	}
	return c, nil
}

type content struct {
	pb.UnimplementedContentServiceServer

	addr string
	root string
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

func (c *content) checkPath(path string) (string, error) {
	dirpath := filepath.Clean(c.root + path)
	if dirpath != c.root && !strings.HasPrefix(dirpath, c.root+"/") {
		return "", status.Errorf(codes.PermissionDenied, "path falls outside root")
	}

	// check if realpath also falls inside root
	dirpath, err := realpath.Realpath(c.root + path)
	if err != nil {
		// try not to return the original path
		if pe, ok := err.(*os.PathError); ok {
			return "", pe.Unwrap()
		} else {
			return "", err
		}
	}
	if dirpath != c.root && !strings.HasPrefix(dirpath, c.root+"/") {
		return "", status.Errorf(codes.PermissionDenied, "path falls outside root")
	}
	return dirpath, nil
}

func (c *content) ReadDir(ctx context.Context, req *pb.ReadDirRequest) (*pb.ReadDirResponse, error) {
	dirpath, err := c.checkPath(req.GetPath())
	if err != nil {
		return nil, err
	}
	dirfiles, err := readdir(dirpath)
	if err != nil {
		return nil, err
	}
	files := []*pb.File{}
	for _, dirfile := range dirfiles {
		file := &pb.File{
			Filename:    dirfile.Name(),
			Hash:        "",
			IsDirectory: dirfile.IsDir(),
		}
		files = append(files, file)
	}
	return &pb.ReadDirResponse{
		Files: files,
	}, nil
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
	path, err := c.checkPath(req.GetFilename())
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
