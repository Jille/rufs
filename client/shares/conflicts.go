package shares

import (
	"context"
	"errors"
	"log"

	pb "github.com/sgielen/rufs/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func handleResolveConflictRequest(ctx context.Context, req *pb.ResolveConflictRequest, circle string) {
	if err := handleResolveConflictRequestImpl(ctx, req, circle); err != nil {
		if st, ok := status.FromError(err); ok && st.Code() != codes.NotFound {
			log.Printf("handleResolveConflictRequest(%q) failed: %v", req.GetFilename(), err)
		}
	}
}

func handleResolveConflictRequestImpl(ctx context.Context, req *pb.ResolveConflictRequest, circle string) error {
	// TODO: Replace this all with a call to StartHash()?
	localPath, err := resolveRemotePath(circle, req.GetFilename())
	if err != nil {
		return err
	}

	h, err := getFileHashWithStat(localPath)
	if err == nil && h != "" {
		// already hashed
		return nil
	}

	select {
	case hashQueue <- hashRequest{local: localPath, remote: req.GetFilename()}:
		return nil
	default:
		return errors.New("hash queue overflow")
	}
}
