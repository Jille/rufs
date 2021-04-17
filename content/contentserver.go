package main

import (
	"context"
	"flag"
	"fmt"
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
)

var (
	port = flag.Int("port", 12010, "listen port")
	path = flag.String("path", "", "root to served content")
)

func main() {
	flag.Parse()

	if *path == "" {
		log.Fatalf("path must not be empty (see -help)")
	}

	rpath, err := realpath.Realpath(*path)
	if err != nil {
		log.Fatalf("failed to resolve content path %s: %v", *path, err)
	}

	c := &content{root: rpath}
	s := grpc.NewServer()
	pb.RegisterContentServiceServer(s, c)
	sock, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	if err := s.Serve(sock); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

type content struct {
	pb.UnimplementedContentServiceServer

	root string
}

func (c *content) ReadDir(ctx context.Context, rq *pb.ReadDirRequest) (*pb.ReadDirResponse, error) {
	dirpath := filepath.Clean(c.root + rq.GetPath())
	if dirpath != c.root && !strings.HasPrefix(dirpath, c.root+"/") {
		return nil, status.Errorf(codes.PermissionDenied, "path falls outside root")
	}

	// check if realpath also falls inside root
	dirpath, err := realpath.Realpath(c.root + rq.GetPath())
	if err != nil {
		// try not to return the original path
		if pe, ok := err.(*os.PathError); ok {
			return nil, pe.Unwrap()
		} else {
			return nil, err
		}
	}
	if dirpath != c.root && !strings.HasPrefix(dirpath, c.root+"/") {
		return nil, status.Errorf(codes.PermissionDenied, "path falls outside root")
	}

	dirfiles, err := os.ReadDir(dirpath)
	if err != nil {
		return nil, err
	}
	files := []*pb.File{}
	for _, dirfile := range dirfiles {
		file := &pb.File{
			Filename: dirfile.Name(),
			Hash:     "",
		}
		files = append(files, file)
	}
	return &pb.ReadDirResponse{
		Files: files,
	}, nil
}

func (c *content) ReadFile(rq *pb.ReadFileRequest, stream pb.ContentService_ReadFileServer) error {
	return status.Errorf(codes.Unimplemented, "method ReadFile not implemented")
}
