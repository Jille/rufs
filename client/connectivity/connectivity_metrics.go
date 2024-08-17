package connectivity

import (
	"context"
	"time"

	"github.com/Jille/rufs/client/metrics"
	pb "github.com/Jille/rufs/proto"
)

func runConnectivityMetrics(ctx context.Context, circle string, client pb.DiscoveryServiceClient) {
	for {
		client.PushMetrics(ctx, &pb.PushMetricsRequest{
			Metrics: metrics.GetAndResetMetrics(circle),
		})

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
