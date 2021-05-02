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

	"github.com/Jille/dfr"
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
	hashQueue    = make(chan hashRequest, 1000)
	hashCacheMtx sync.Mutex
	hashCache    = map[string]cachedHash{}
)

type hashRequest struct {
	local    string
	remote   string
	download *pb.ConnectResponse_ActiveDownload
}

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
			activeReads: map[string]int{},
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
	mtx         sync.Mutex
	activeReads map[string]int
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

func (c *content) getPeerAndCircle(ctx context.Context) (string, *config.Circle, error) {
	peer, circle, err := security.PeerFromContext(ctx)
	if err != nil {
		return "", nil, err
	}
	circ, ok := config.GetCircle(circle)
	if !ok {
		return "", nil, status.Error(codes.NotFound, "no shares configured for this circle")
	}
	return peer, circ, nil
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
			if os.IsNotExist(pe) {
				return "", status.Error(codes.NotFound, pe.Unwrap().Error())
			} else {
				return "", pe.Unwrap()
			}
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
	_, circle, err := c.getPeerAndCircle(ctx)
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

func (c *content) ReadFile(req *pb.ReadFileRequest, stream pb.ContentService_ReadFileServer) (retErr error) {
	d := dfr.D{}
	defer d.Run(&retErr)
	peer, circle, err := c.getPeerAndCircle(stream.Context())
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

	circleState.mtx.Lock()
	circleState.activeReads[path]++
	upgrade := circleState.activeReads[path] > 1
	circleState.mtx.Unlock()
	defer func() {
		circleState.mtx.Lock()
		circleState.activeReads[path]--
		circleState.mtx.Unlock()
	}()

	t := transfer.GetActiveTransfer(circle.Name, reqpath)

	if upgrade || (t != nil && t.DownloadId() != 0) {
		h, err := c.getFileHashWithStat(path)
		if err != nil {
			h = ""
		}

		if t == nil {
			t, err = transfer.OpenLocalFile(reqpath, path, h, circle.Name)
			if err != nil {
				metrics.AddContentOrchestrationJoinFailed([]string{circle.Name}, "busy-file", 1)
				return err
			}
		} else if t.GetHash() != "" && h != "" && t.GetHash() != h {
			goto fallbackToActive
		} else if t.TransferIsRemote() {
			err := t.SetLocalFile(path, h)
			if err != nil {
				metrics.AddContentOrchestrationJoinFailed([]string{circle.Name}, "busy-file", 1)
				return err
			}
		}

		if t.DownloadId() == 0 {
			if err := t.SwitchToOrchestratedMode(0); err != nil {
				metrics.AddContentOrchestrationJoinFailed([]string{circle.Name}, "busy-file", 1)
				return err
			}
			metrics.AddContentOrchestrationJoined([]string{circle.Name}, "busy-file", 1)

			select {
			case hashQueue <- hashRequest{
				local:  path,
				remote: reqpath,
				download: &pb.ConnectResponse_ActiveDownload{
					DownloadId: t.DownloadId(),
				},
			}:
			default:
				log.Println("Error while starting orchestration: hash queue overflow")
			}
		}

		id := t.DownloadId()
		if id == 0 {
			panic("Wanted to redirect to download_id 0")
		}
		if err := stream.Send(&pb.ReadFileResponse{
			RedirectToOrchestratedDownload: id,
		}); err != nil {
			return err
		}
		return nil
	}

fallbackToActive:
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
		metrics.AddTransferSendBytes([]string{circle.Name}, peer, "simple", n)
		if n < r {
			// Short read, so we hit EOF.
			return nil
		}
		offset += n
		remaining -= n
	}
}

func (c *content) PassiveTransfer(stream pb.ContentService_PassiveTransferServer) error {
	_, circle, err := c.getPeerAndCircle(stream.Context())
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

	at := transfer.GetTransferForDownloadId(circle.Name, d)
	if at == nil {
		return fmt.Errorf("download_id %d is not known (yet?) at this side, please ring later", d)
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
	case hashQueue <- hashRequest{local: path, remote: reqpath}:
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

func (c *content) handleActiveDownloadListImpl(ctx context.Context, req *pb.ConnectResponse_ActiveDownloadList, circle string) (retErr error) {
	d := dfr.D{}
	defer d.Run(&retErr)
	circ, ok := config.GetCircle(circle)
	if !ok {
		return status.Error(codes.NotFound, "no shares configured for this circle")
	}

	shares := circ.Shares

	for _, activeDownload := range req.GetActiveDownloads() {
		if t := transfer.GetTransferForDownloadId(circ.Name, activeDownload.GetDownloadId()); t != nil {
			// This active-download isn't new to us
			continue
		}

		remotePath := ""
		localPath := ""
		for _, path := range activeDownload.GetFilenames() {
			l, err := c.getLocalPath(shares, path)
			if err == nil {
				remotePath = path
				localPath = l
				break
			}
		}

		if localPath == "" {
			continue
		}

		// Ensure that if a content server is already in this orchestration, they start hashing
		if activeDownload.GetHash() == "" {
			if _, err := connectivity.DiscoveryClient(circle).ResolveConflict(ctx, &pb.ResolveConflictRequest{
				Filename: remotePath,
			}); err != nil {
				log.Printf("Failed to start conflict resolution for %q: %v", remotePath, err)
			}
		}

		// We will hash the file as well, in case we have the same one
		h, err := c.getFileHashWithStat(localPath)
		if err != nil {
			log.Printf("Error while joining active download: couldn't determine hash for %q: %v", localPath, err)
			continue
		}
		if h == "" {
			select {
			case hashQueue <- hashRequest{
				local:    localPath,
				remote:   remotePath,
				download: activeDownload,
			}:
			default:
				log.Println("Error while joining active download: hash queue overflow")
			}
			continue
		}

		if activeDownload.GetHash() == "" {
			log.Printf("Won't join active download: transfer hash unknown for %q", remotePath)
			continue
		} else if h != activeDownload.GetHash() {
			log.Printf("Won't join active download: hash mismatch for %q (%s vs %s)", remotePath, h, activeDownload.GetHash())
			continue
		}

		t, err := transfer.OpenLocalFile(remotePath, localPath, h, circ.Name)
		if err != nil {
			log.Printf("Error while joining active download: error while creating *transfer.Transfer: %v", err)
			metrics.AddContentOrchestrationJoinFailed([]string{circ.Name}, "active", 1)
		} else if err := t.SwitchToOrchestratedMode(activeDownload.GetDownloadId()); err != nil {
			log.Printf("Error while joining active download: error while switching to orchestrated mode: %v", err)
			metrics.AddContentOrchestrationJoinFailed([]string{circ.Name}, "active", 1)
		} else {
			metrics.AddContentOrchestrationJoined([]string{circ.Name}, "active", 1)
		}
	}
	return nil
}

func (c *content) hashWorker() {
	for req := range hashQueue {
		hash, err := c.hashFile(req.local)
		if err != nil {
			log.Printf("Failed to hash %q: %v", req.local, err)
			continue
		}
		for name := range c.circles {
			t := transfer.GetActiveTransfer(name, req.remote)
			if t != nil {
				if t.GetHash() == "" {
					t.SetHash(hash)
				} else if t.DownloadId() != 0 && t.GetHash() != hash {
					log.Fatalf("Orchestration mode collision for %q: Hash mismatch (%s vs %s)", req.local, t.GetHash(), hash)
				}
			} else if req.download != nil && hash == req.download.GetHash() {
				t, err = transfer.OpenLocalFile(req.remote, req.local, hash, name)
				if err != nil {
					metrics.AddContentOrchestrationJoinFailed([]string{name}, "hashed", 1)
					log.Printf("Failed to join orchestration after hashing %q: %v", req.local, err)
				} else if err := t.SwitchToOrchestratedMode(req.download.GetDownloadId()); err != nil {
					metrics.AddContentOrchestrationJoinFailed([]string{name}, "hashed", 1)
					log.Printf("Failed to switch to orchestrated mode after hashing %q: %v", req.local, err)
				} else {
					metrics.AddContentOrchestrationJoined([]string{name}, "hashed", 1)
				}
			}
			metrics.AddContentHashes([]string{name}, 1)
		}
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

func (c *content) hashFile(fn string) (string, error) {
	fh, err := os.Open(fn)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return "", err
	}
	if h := c.getFileHash(fn, st); h != "" {
		// We already have the latest hash.
		return h, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", err
	}
	hash := fmt.Sprintf("%x", h.Sum(nil))
	hashCacheMtx.Lock()
	hashCache[fn] = cachedHash{
		hash:  hash,
		mtime: st.ModTime(),
		size:  st.Size(),
	}
	hashCacheMtx.Unlock()
	return hash, nil
}
