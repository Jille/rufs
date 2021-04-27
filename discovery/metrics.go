package main

import (
	"context"

	pb "github.com/sgielen/rufs/proto"
)

func (d *discovery) PushMetrics(ctx context.Context, req *pb.PushMetricsRequest) (*pb.PushMetricsResponse, error) {
	return &pb.PushMetricsResponse{}, nil
}
