package content

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/config"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
	"github.com/yookoala/realpath"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

var (
	hashQueue    = make(chan string, 1000)
	hashCacheMtx sync.Mutex
	hashCache    = map[string]cachedHash{}
)

type cachedHash struct {
	hash  string
	mtime time.Time
	size  uint64
}

func New(addr string, kps []*security.KeyPair) (*content, error) {
	if addr == "" {
		return nil, errors.New("missing parameter addr")
	}

	for _, circle := range config.GetCircles() {
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
		addr:     addr,
		keyPairs: kps,
	}
	go c.hashWorker()
	connectivity.HandleResolveConflictRequest = c.handleResolveConflictRequest
	return c, nil
}

type content struct {
	pb.UnimplementedContentServiceServer

	addr     string
	keyPairs []*security.KeyPair
}

func (c *content) Run() {
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(security.TLSConfigForServer(c.keyPairs))))
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

func (c *content) getSharesForPeer(ctx context.Context) ([]*config.Share, error) {
	_, circle, err := security.PeerFromContext(ctx)
	if err != nil {
		return nil, err
	}
	circ, ok := config.GetCircle(circle)
	if !ok {
		return nil, status.Error(codes.NotFound, "no shares configured for this circle")
	}
	return circ.Shares, nil
}

func (c *content) getLocalPath(shares []*config.Share, path string) (string, error) {
	// find matching share
	remote := strings.Split(path, "/")[0]
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

	path = strings.TrimLeft(path[len(remote):], "/")

	root := matchingShare.Local
	dirpath := filepath.Clean(root + "/" + path)
	if dirpath != root && !strings.HasPrefix(dirpath, root+"/") {
		return "", status.Errorf(codes.PermissionDenied, "path falls outside root")
	}

	// check if realpath also falls inside root
	dirpath, err := realpath.Realpath(dirpath)
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
	shares, err := c.getSharesForPeer(ctx)
	if err != nil {
		return nil, err
	}

	reqpath := strings.TrimLeft(req.GetPath(), "/")

	res := &pb.ReadDirResponse{}

	if reqpath == "" {
		for _, share := range shares {
			file := &pb.File{
				Filename:    share.Remote,
				IsDirectory: true,
				Mtime:       time.Now().Unix(),
			}
			res.Files = append(res.Files, file)
		}
		return res, nil
	}

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
			IsDirectory: dirfile.IsDir(),
			Size:        uint64(dirfile.Size()),
			Mtime:       dirfile.ModTime().Unix(),
		}
		hashCacheMtx.Lock()
		if e, ok := hashCache[filepath.Join(dirpath, dirfile.Name())]; ok {
			if e.size == file.Size && e.mtime.Unix() == file.Mtime {
				file.Hash = e.hash
			} else {
				delete(hashCache, filepath.Join(dirpath, dirfile.Name()))
			}
		}
		hashCacheMtx.Unlock()
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
	shares, err := c.getSharesForPeer(stream.Context())
	if err != nil {
		return err
	}

	reqpath := strings.TrimLeft(req.GetFilename(), "/")

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

func (c *content) handleResolveConflictRequest(ctx context.Context, req *pb.ResolveConflictRequest, circle string) {
	if err := c.handleResolveConflictRequestImpl(ctx, req, circle); err != nil {
		log.Printf("handleResolveConflictRequest(%q) failed: %v", req.GetFilename(), err)
	}
}

func (c *content) handleResolveConflictRequestImpl(ctx context.Context, req *pb.ResolveConflictRequest, circle string) error {
	circ, ok := config.GetCircle(circle)
	if !ok {
		return status.Error(codes.NotFound, "no shares configured for this circle")
	}
	shares := circ.Shares

	reqpath := strings.TrimLeft(req.GetFilename(), "/")

	if reqpath == "" {
		return status.Errorf(codes.FailedPrecondition, "is a directory")
	}

	path, err := c.getLocalPath(shares, reqpath)
	if err != nil {
		return err
	}
	select{
	case hashQueue <- path:
		return nil
	default:
		return errors.New("hash queue overflow")
	}
}

func (c *content) hashWorker() {
	for fn := range hashQueue {
		if err := c.hashFile(fn); err != nil {
			log.Printf("Failed to hash %q: %v", fn, err)
		}
	}
}

func (c *content) hashFile(fn string) error {
	fh, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return err
	}
	hash := fmt.Sprintf("%x", h.Sum(nil))
	hashCacheMtx.Lock()
	hashCache[fn] = cachedHash{
		hash:  hash,
		mtime: st.ModTime(),
		size:  uint64(st.Size()),
	}
	hashCacheMtx.Unlock()
	return nil
}
