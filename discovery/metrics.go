package main

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	pb "github.com/sgielen/rufs/proto"
	"github.com/sgielen/rufs/security"
)

type processMetric func(peer string, m *pb.PushMetricsRequest_Metric)

var (
	metrics = map[pb.PushMetricsRequest_MetricId]processMetric{
		pb.PushMetricsRequest_CLIENT_START_TIME_US: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "client_start_time_us",
			Help:      "Microseconds since the epoch at which this client started",
		}, nil),
	}
)

func newCounter(opts prometheus.CounterOpts, labelNames []string) processMetric {
	cv := promauto.NewCounterVec(opts, append([]string{"peer"}, labelNames...))
	return func(peer string, m *pb.PushMetricsRequest_Metric) {
		cv.WithLabelValues(append([]string{peer}, m.GetFields()...)...).Add(m.GetSingleValue())
	}
}

func newGauge(opts prometheus.GaugeOpts, labelNames []string) processMetric {
	gv := promauto.NewGaugeVec(opts, append([]string{"peer"}, labelNames...))
	return func(peer string, m *pb.PushMetricsRequest_Metric) {
		gv.WithLabelValues(append([]string{peer}, m.GetFields()...)...).Set(m.GetSingleValue())
	}
}

func (d *discovery) PushMetrics(ctx context.Context, req *pb.PushMetricsRequest) (*pb.PushMetricsResponse, error) {
	peer, _, err := security.PeerFromContext(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range req.GetMetrics() {
		md, ok := metrics[m.GetId()]
		if !ok {
			continue
		}
		md(peer, m)
	}
	return &pb.PushMetricsResponse{}, nil
}

func serveMetrics() {
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":12001", nil)
}
