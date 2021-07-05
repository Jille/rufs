package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	pb "github.com/sgielen/rufs/proto"
)

var (
	defaultBuckets = prometheus.ExponentialBuckets(0.004, 2, 17)
	// TODO(quis): Make this a conscious decision.
	bucketsForVfsOpenLatency         = defaultBuckets
	bucketsForTransferReadSizes      = defaultBuckets
	bucketsForTransferReadLatency    = defaultBuckets
	bucketsForVfsReaddirLatency      = defaultBuckets
	bucketsForVfsPeerReaddirLatency  = defaultBuckets
	bucketsForContentRpcsRecvLatency = defaultBuckets
)

type processMetric func(peer string, m *pb.PushMetricsRequest_Metric)

func newCounter(opts prometheus.CounterOpts, labelNames []string) processMetric {
	cv := promauto.NewCounterVec(opts, append([]string{"client"}, labelNames...))
	return func(client string, m *pb.PushMetricsRequest_Metric) {
		cv.WithLabelValues(append([]string{client}, m.GetFields()...)...).Add(m.GetSingleValue())
	}
}

func newGauge(opts prometheus.GaugeOpts, labelNames []string) processMetric {
	gv := promauto.NewGaugeVec(opts, append([]string{"client"}, labelNames...))
	return func(client string, m *pb.PushMetricsRequest_Metric) {
		gv.WithLabelValues(append([]string{client}, m.GetFields()...)...).Set(m.GetSingleValue())
	}
}

func newHistogram(opts prometheus.HistogramOpts, labelNames []string) processMetric {
	hv := promauto.NewHistogramVec(opts, append([]string{"client"}, labelNames...))
	return func(client string, m *pb.PushMetricsRequest_Metric) {
		o := hv.WithLabelValues(append([]string{client}, m.GetFields()...)...)
		for _, v := range m.GetNewDistributionValues() {
			o.Observe(v)
		}
	}
}

func PushMetrics(peer string, req *pb.PushMetricsRequest) {
	for _, m := range req.GetMetrics() {
		md, ok := metrics[m.GetId()]
		if !ok {
			continue
		}
		md(peer, m)
	}
}

func Serve() {
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":12001", nil)
}
