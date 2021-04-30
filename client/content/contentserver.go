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
	"github.com/sgielen/rufs/client/metrics"
	"github.com/sgielen/rufs/client/transfer"
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
	size  int64
}

func New(addr string, kps []*security.KeyPair) (*content, error) {
	if addr == "" {
		return nil, errors.New("missing parameter addr")
	}

	c := &content{
		addr:     addr,
		keyPairs: kps,
		circles:  map[string]*circleState{},
	}

	for _, circle := range config.GetCircles() {
		c.circles[circle.Name] = &circleState{
			activeReads:     map[string]int{},
			activeTransfers: map[string]*transfer.Transfer{},
		}

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

	go c.hashWorker()
	connectivity.HandleResolveConflictRequest = c.handleResolveConflictRequest
	connectivity.HandleActiveDownloadList = c.handleActiveDownloadList
	return c, nil
}

type content struct {
	pb.UnimplementedContentServiceServer

	addr     string
	keyPairs []*security.KeyPair
	circles  map[string]*circleState
}

type circleState struct {
	activeTransfersMtx sync.Mutex
	activeReads        map[string]int
	activeTransfers    map[string]*transfer.Transfer
}

func (c *circleState) getTransferForDownloadId(downloadId int64) *transfer.Transfer {
	c.activeTransfersMtx.Lock()
	defer c.activeTransfersMtx.Unlock()
	for _, t := range c.activeTransfers {
		if t.DownloadId() == downloadId {
			return t
		}
	}
	return nil
}

func (c *content) Run() {
	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(security.TLSConfigForServer(c.keyPairs))), grpc.ChainUnaryInterceptor(c.unaryInterceptor), grpc.ChainStreamInterceptor(c.streamInterceptor))
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

func (c *content) unaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	peer, circle, err := security.PeerFromContext(ctx)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := handler(ctx, req)
	d := time.Since(start)
	metrics.AddContentRpcsRecv([]string{circle}, info.FullMethod, peer, status.Code(err).String(), 1)
	metrics.AppendContentRpcsRecvLatency([]string{circle}, info.FullMethod, peer, status.Code(err).String(), d.Seconds())
	return resp, err
}

func (c *content) streamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	peer, circle, err := security.PeerFromContext(ss.Context())
	if err != nil {
		return err
	}
	start := time.Now()
	err = handler(srv, ss)
	d := time.Since(start)
	metrics.AddContentRpcsRecv([]string{circle}, info.FullMethod, peer, status.Code(err).String(), 1)
	metrics.AppendContentRpcsRecvLatency([]string{circle}, info.FullMethod, peer, status.Code(err).String(), d.Seconds())
	return err
}

func (c *content) getCircleForPeer(ctx context.Context) (*config.Circle, error) {
	_, circle, err := security.PeerFromContext(ctx)
	if err != nil {
		return nil, err
	}
	circ, ok := config.GetCircle(circle)
	if !ok {
		return nil, status.Error(codes.NotFound, "no shares configured for this circle")
	}
	return circ, nil
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
	circle, err := c.getCircleForPeer(ctx)
	if err != nil {
		return nil, err
	}

	reqpath := strings.TrimLeft(req.GetPath(), "/")

	res := &pb.ReadDirResponse{}

	if reqpath == "" {
		for _, share := range circle.Shares {
			file := &pb.File{
				Filename:    share.Remote,
				IsDirectory: true,
				Mtime:       time.Now().Unix(),
			}
			res.Files = append(res.Files, file)
		}
		return res, nil
	}

	dirpath, err := c.getLocalPath(circle.Shares, reqpath)
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
			Size:        dirfile.Size(),
			Mtime:       dirfile.ModTime().Unix(),
		}
		if h := c.getFileHash(filepath.Join(dirpath, dirfile.Name()), dirfile); h != "" {
			file.Hash = h
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
	circle, err := c.getCircleForPeer(stream.Context())
	if err != nil {
		return err
	}

	reqpath := strings.TrimLeft(req.GetFilename(), "/")

	if reqpath == "" {
		return status.Errorf(codes.FailedPrecondition, "is a directory")
	}

	path, err := c.getLocalPath(circle.Shares, reqpath)
	if err != nil {
		return err
	}

	circleState := c.circles[circle.Name]

	circleState.activeTransfersMtx.Lock()
	circleState.activeReads[path]++
	upgrade := circleState.activeReads[path] > 1
	t := circleState.activeTransfers[path]
	defer func() {
		circleState.activeTransfersMtx.Lock()
		circleState.activeReads[path]--
		circleState.activeTransfersMtx.Unlock()
	}()
	if upgrade && t == nil {
		h, err := c.getFileHashWithStat(path)
		if err != nil {
			h = ""
		}
		t, err = transfer.NewLocalFile(reqpath, path, h, circle.Name)
		if err != nil {
			metrics.AddContentOrchestrationJoinFailed([]string{circle.Name}, "busy-file", 1)
			return err
		}
		if err := t.SwitchToOrchestratedMode(0); err != nil {
			t.Close()
			metrics.AddContentOrchestrationJoinFailed([]string{circle.Name}, "busy-file", 1)
			return err
		}
		circleState.activeTransfers[path] = t
		metrics.AddContentOrchestrationJoined([]string{circle.Name}, "busy-file", 1)
	}
	circleState.activeTransfersMtx.Unlock()
	if t != nil {
		if err := stream.Send(&pb.ReadFileResponse{
			RedirectToOrchestratedDownload: t.DownloadId(),
		}); err != nil {
			return err
		}
		return nil
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
		if r > int64(len(buf)) {
			r = int64(len(buf))
		}
		rn, err := fh.ReadAt(buf[:r], offset)
		if err != nil && err != io.EOF {
			return status.Errorf(codes.ResourceExhausted, "failed to read from %q at %d: %v", req.GetFilename(), offset, err)
		}
		n := int64(rn)
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

func (c *content) PassiveTransfer(stream pb.ContentService_PassiveTransferServer) error {
	circle, err := c.getCircleForPeer(stream.Context())
	if err != nil {
		return err
	}

	msg, err := stream.Recv()
	if err != nil {
		return err
	}
	d := msg.GetDownloadId()
	if d == 0 {
		return errors.New("download_id should be set (and nothing else)")
	}

	circleState := c.circles[circle.Name]
	at := circleState.getTransferForDownloadId(d)
	if at == nil {
		return errors.New("that download_id is not known (yet?) at this side, please ring later")
	}
	return at.HandleIncomingPassiveTransfer(stream)
}

func (c *content) handleResolveConflictRequest(ctx context.Context, req *pb.ResolveConflictRequest, circle string) {
	if err := c.handleResolveConflictRequestImpl(ctx, req, circle); err != nil {
		if st, ok := status.FromError(err); ok && st.Code() != codes.NotFound {
			log.Printf("handleResolveConflictRequest(%q) failed: %v", req.GetFilename(), err)
		}
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

	h, err := c.getFileHashWithStat(path)
	if err == nil && h != "" {
		// already hashed
		return nil
	}

	select {
	case hashQueue <- path:
		return nil
	default:
		return errors.New("hash queue overflow")
	}
}

func (c *content) handleActiveDownloadList(ctx context.Context, req *pb.ConnectResponse_ActiveDownloadList, circle string) {
	if err := c.handleActiveDownloadListImpl(ctx, req, circle); err != nil {
		log.Printf("handleActiveDownloadList() failed: %v", err)
	}
}

func (c *content) handleActiveDownloadListImpl(ctx context.Context, req *pb.ConnectResponse_ActiveDownloadList, circle string) error {
	circ, ok := config.GetCircle(circle)
	if !ok {
		return status.Error(codes.NotFound, "no shares configured for this circle")
	}

	shares := circ.Shares
	circleState := c.circles[circ.Name]

	for _, activeDownload := range req.GetActiveDownloads() {
		if t := circleState.getTransferForDownloadId(activeDownload.GetDownloadId()); t != nil {
			// This active-download isn't new to us
			continue
		}

		remotePath := ""
		localPath := ""
		for _, path := range activeDownload.GetFilenames() {
			l, err := c.getLocalPath(shares, path)
			if err != nil {
				remotePath = path
				localPath = l

				if activeDownload.GetHash() == "" {
					if _, err := connectivity.DiscoveryClient(circle).ResolveConflict(ctx, &pb.ResolveConflictRequest{
						Filename: path,
					}); err != nil {
						log.Printf("Failed to start conflict resolution for %q: %v", path, err)
					}
				}
			}
		}
		if localPath == "" {
			continue
		}

		h, err := c.getFileHashWithStat(localPath)
		if err != nil {
			log.Printf("Error while joining active download: couldn't determine hash for %q: %v", localPath, err)
			continue
		}
		if h == "" {
			select {
			case hashQueue <- localPath:
			default:
				log.Println("Error while joining active download: hash queue overflow")
			}
			continue
		}

		if h != activeDownload.GetHash() {
			log.Printf("Won't join active download: hash mismatch for %q (%s vs %s)", remotePath, h, activeDownload.GetHash())
			continue
		}

		circleState.activeTransfersMtx.Lock()
		if circleState.activeTransfers[localPath] == nil {
			t, err := transfer.NewLocalFile(remotePath, localPath, h, circ.Name)
			if err != nil {
				log.Printf("Error while joining active download: error while creating *transfer.Transfer: %v", err)
				circleState.activeTransfersMtx.Unlock()
				metrics.AddContentOrchestrationJoinFailed([]string{circ.Name}, "active", 1)
				continue
			}
			if err := t.SwitchToOrchestratedMode(0); err != nil {
				log.Printf("Error while joining active download: error while switching to orchestrated mode: %v", err)
				t.Close()
				circleState.activeTransfersMtx.Unlock()
				metrics.AddContentOrchestrationJoinFailed([]string{circ.Name}, "active", 1)
				continue
			}
			circleState.activeTransfers[localPath] = t
			metrics.AddContentOrchestrationJoined([]string{circ.Name}, "active", 1)
		}
		circleState.activeTransfersMtx.Unlock()
	}
	return nil
}

func (c *content) hashWorker() {
	for fn := range hashQueue {
		if err := c.hashFile(fn); err != nil {
			log.Printf("Failed to hash %q: %v", fn, err)
		}
		// TODO: if any orchestrations exist for this file, update the hash in those orchestrations
	}
}

func (c *content) getFileHashWithStat(fn string) (string, error) {
	var err error
	st, err := os.Stat(fn)
	if err != nil {
		return "", err
	}
	return c.getFileHash(fn, st), nil
}

func (c *content) getFileHash(fn string, st os.FileInfo) string {
	hashCacheMtx.Lock()
	defer hashCacheMtx.Unlock()
	h, ok := hashCache[fn]
	if ok && h.mtime == st.ModTime() && h.size == st.Size() {
		return h.hash
	}
	if ok {
		delete(hashCache, fn)
	}
	return ""
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
	if c.getFileHash(fn, st) != "" {
		// We already have the latest hash.
		return nil
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
		size:  st.Size(),
	}
	hashCacheMtx.Unlock()
	for name, circ := range c.circles {
		circ.activeTransfersMtx.Lock()
		if t, ok := circ.activeTransfers[fn]; ok {
			t.SetHash(hash)
		}
		circ.activeTransfersMtx.Unlock()
		metrics.AddContentHashes([]string{name}, 1)
	}
	return nil
}
