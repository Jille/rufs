package shares

import (
	"context"

	pb "github.com/sgielen/rufs/proto"
)

func handleResolveConflictRequest(ctx context.Context, req *pb.ResolveConflictRequest, circle string) {
	StartHash(circle, req.GetFilename())
}
