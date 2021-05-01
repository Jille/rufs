package transfers

import (
	"context"
	"errors"

	"github.com/sgielen/rufs/client/transfer"
	pb "github.com/sgielen/rufs/proto"
)

func HandleActiveDownloadList(ctx context.Context, req *pb.ConnectResponse_ActiveDownloadList, circle string) {
}

func SwitchToOrchestratedMode(circle, remoteFilename string) (int64, error) {
	return 0, errors.New("not yet implemented")
}

func GetTransferForFile(f *pb.File) (*transfer.Transfer, error) {
	return nil, errors.New("not yet implemented")
}
