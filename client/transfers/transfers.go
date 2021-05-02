package transfers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/sgielen/rufs/client/connectivity"
	"github.com/sgielen/rufs/client/metrics"
	"github.com/sgielen/rufs/client/shares"
	"github.com/sgielen/rufs/client/transfer"
	"github.com/sgielen/rufs/common"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
)

var (
	mtx     sync.Mutex
	circles = map[string]*circle{}
)

type circle struct {
	name                       string
	byId                       map[int64]*transfer.Transfer
	byRemoteFilename           map[string]*transfer.Transfer
	interestingActiveDownloads map[string]*pb.ConnectResponse_ActiveDownload
}

func init() {
	connectivity.HandleActiveDownloadList = HandleActiveDownloadList
	transfer.ForgetCallback = Forget
	transfer.RedirectToOrchestrationCallback = RedirectToOrchestration
}

func getCircle(name string) *circle {
	if c, ok := circles[name]; ok {
		return c
	}
	c := &circle{
		name:                       name,
		byId:                       map[int64]*transfer.Transfer{},
		byRemoteFilename:           map[string]*transfer.Transfer{},
		interestingActiveDownloads: map[string]*pb.ConnectResponse_ActiveDownload{},
	}
	circles[name] = c
	shares.RegisterHashListener(c.hashListener)
	return c
}

func GetTransferForFile(ctx context.Context, remoteFilename, maybeHash string, size int64, peers []*connectivity.Peer) (*transfer.Transfer, error) {
	mtx.Lock()
	defer mtx.Unlock()
	c := getCircle(common.CircleFromPeer(peers[0].Name))
	if t, ok := c.byRemoteFilename[remoteFilename]; ok {
		return t, nil
	}
	t, err := transfer.NewRemoteFile(ctx, remoteFilename, maybeHash, size, peers)
	if err != nil {
		return nil, err
	}
	c.byRemoteFilename[remoteFilename] = t
	return t, nil
}

func IsLocalFileOrchestrated(circle, remoteFilename string) (int64, bool) {
	mtx.Lock()
	defer mtx.Unlock()
	c := getCircle(circle)
	t, ok := c.byRemoteFilename[remoteFilename]
	if !ok {
		return 0, false
	}
	// TODO(quis): Verify this is the same file we have locally.
	d := t.DownloadId()
	if d == 0 {
		return 0, false
	}
	return d, true
}

func SwitchToOrchestratedMode(circle, remoteFilename string) (int64, error) {
	mtx.Lock()
	defer mtx.Unlock()
	shares.StartHash(circle, remoteFilename)
	c := getCircle(circle)
	t, err := c.makeTransferWithLocalfile(c.byRemoteFilename[remoteFilename], remoteFilename, "")
	if err != nil {
		log.Printf("Error starting orchestration mode for %q: %v", remoteFilename, err)
		metrics.AddContentOrchestrationJoinFailed([]string{c.name}, "busy-file", 1)
		return 0, err
	}
	if err := t.SwitchToOrchestratedMode(0); err != nil {
		log.Printf("Error starting orchestration mode for %q: error while switching to orchestrated mode: %v", remoteFilename, err)
		metrics.AddContentOrchestrationJoinFailed([]string{c.name}, "busy-file", 1)
		return 0, err
	}
	metrics.AddContentOrchestrationJoined([]string{c.name}, "busy-file", 1)
	id := t.DownloadId()
	if id == 0 {
		panic("Just joined a download and now its id is 0?")
	}
	log.Printf("Have created %d on request of our contentserver", id)
	c.byId[id] = t
	c.byRemoteFilename[remoteFilename] = t
	return id, nil
}

func HandleActiveDownloadList(ctx context.Context, req *pb.ConnectResponse_ActiveDownloadList, circle string) {
	mtx.Lock()
	defer mtx.Unlock()
	c := getCircle(circle)
	for _, ad := range req.GetActiveDownloads() {
		if _, found := c.byId[ad.GetDownloadId()]; found {
			continue
		}
		remoteFilename, found := c.findPathForActiveDownload(ad)
		if !found {
			// We don't have a file with this name.
			continue
		}
		if ad.GetHash() == "" {
			c.calculateRemoteActiveDownloadHash(ctx, ad)
		} else {
			c.interestingActiveDownloads[remoteFilename] = ad
		}
		shares.StartHash(c.name, remoteFilename)
	}
}

func (c *circle) findPathForActiveDownload(ad *pb.ConnectResponse_ActiveDownload) (string, bool) {
	for _, remoteFilename := range ad.GetFilenames() {
		_, err := shares.Stat(c.name, remoteFilename)
		if err != nil {
			continue
		}
		return remoteFilename, true
	}
	return "", false
}

func (c *circle) calculateRemoteActiveDownloadHash(ctx context.Context, ad *pb.ConnectResponse_ActiveDownload) {
	if _, err := connectivity.DiscoveryClient(c.name).ResolveConflict(ctx, &pb.ResolveConflictRequest{
		Filename: ad.GetFilenames()[0],
	}); err != nil {
		log.Printf("Failed to start conflict resolution for %q: %v", ad.GetFilenames()[0], err)
	}
}

func (c *circle) hashListener(circle, remoteFilename, hash string) {
	if circle != c.name {
		return
	}
	mtx.Lock()
	defer mtx.Unlock()
	if t, ok := c.byRemoteFilename[remoteFilename]; ok {
		t.SetHash(hash)
	}

	ad, ok := c.interestingActiveDownloads[remoteFilename]
	if !ok {
		return
	}
	if hash != ad.GetHash() {
		delete(c.interestingActiveDownloads, remoteFilename)
		return
	}
	t, err := c.makeTransferWithLocalfile(c.byId[ad.GetDownloadId()], remoteFilename, hash)
	if err != nil {
		log.Printf("Error while joining active download: %v", err)
		metrics.AddContentOrchestrationJoinFailed([]string{c.name}, "busy-file", 1)
		return
	}
	if err := t.SwitchToOrchestratedMode(ad.GetDownloadId()); err != nil {
		log.Printf("Error while joining active download %q: error while switching to orchestrated mode: %v", remoteFilename, err)
		metrics.AddContentOrchestrationJoinFailed([]string{c.name}, "busy-file", 1)
		return
	}
	metrics.AddContentOrchestrationJoined([]string{c.name}, "busy-file", 1)
	log.Printf("hashListener(%q, %q, %q): joined %s", circle, remoteFilename, hash, ad)
	c.byId[ad.GetDownloadId()] = t
	c.byRemoteFilename[remoteFilename] = t
}

func (c *circle) makeTransferWithLocalfile(t *transfer.Transfer, remoteFilename, maybeHash string) (*transfer.Transfer, error) {
	if t == nil || t.TransferIsRemote() {
		fh, err := shares.Open(c.name, remoteFilename)
		if err != nil {
			return nil, fmt.Errorf("shares.Open(%q, %q): %v", c.name, remoteFilename, err)
		}
		if t == nil {
			t, err = transfer.NewLocalFile(remoteFilename, fh, maybeHash, c.name)
			if err != nil {
				return nil, fmt.Errorf("transfer.NewLocalFile(): %v", err)
			}
		} else {
			if err := t.SetLocalFile(fh, maybeHash); err != nil {
				return nil, fmt.Errorf("transfer.SetLocalFile(): %v", err)
			}
		}
	}
	return t, nil
}

func RedirectToOrchestration(circle string, t *transfer.Transfer, downloadId int64) error {
	mtx.Lock()
	defer mtx.Unlock()
	c := getCircle(circle)
	log.Printf("Transfer got redirected to orchestration %d", t.DownloadId())
	if err := t.SwitchToOrchestratedMode(downloadId); err != nil {
		metrics.AddContentOrchestrationJoinFailed([]string{c.name}, "redirected", 1)
		return err
	}
	c.byId[downloadId] = t
	return nil
}

func HandleIncomingPassiveTransfer(stream pb.ContentService_PassiveTransferServer) error {
	_, circle, err := security.PeerFromContext(stream.Context())
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

	mtx.Lock()
	c := getCircle(circle)
	t, ok := c.byId[d]
	mtx.Unlock()
	if !ok {
		log.Printf("HandleIncomingPassiveTransfer: Refusing because we don't know %d yet", d)
		return fmt.Errorf("download_id %d is not known (yet?) at this side, please ring later", d)
	}
	return t.HandleIncomingPassiveTransfer(stream)
}

func Forget(circle string, t *transfer.Transfer) {
	mtx.Lock()
	defer mtx.Unlock()
	c := getCircle(circle)
	// TODO: Use map lookups.
	for id, ot := range c.byId {
		if ot == t {
			delete(c.byId, id)
			break
		}
	}
	for fn, ot := range c.byRemoteFilename {
		if ot == t {
			delete(c.byRemoteFilename, fn)
			break
		}
	}
}
