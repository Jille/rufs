package metrics

// This file is generated by metricgen/gen.go.
import (
	"github.com/prometheus/client_golang/prometheus"
	pb "github.com/sgielen/rufs/proto"
)

var (
	metrics = map[pb.PushMetricsRequest_MetricId]processMetric{
		pb.PushMetricsRequest_CLIENT_START_TIME_SECONDS: newGauge(prometheus.GaugeOpts{
			Namespace: "rufs",
			Name:      "client_start_time_seconds",
			Help:      "Timestamp at which each client started",
		}, nil),
		pb.PushMetricsRequest_TRANSFER_READS_ACTIVE: newGauge(prometheus.GaugeOpts{
			Namespace: "rufs",
			Name:      "transfer_reads_active",
			Help:      "Number of currently active reads",
		}, nil),
		pb.PushMetricsRequest_TRANSFER_OPENS: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "transfer_opens",
			Help:      "Number of open calls from the VFS",
		}, []string{"code"}),
		pb.PushMetricsRequest_TRANSFER_READS: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "transfer_reads",
			Help:      "Number of read calls from the VFS",
		}, []string{"code"}),
		pb.PushMetricsRequest_TRANSFER_READ_SIZES: newHistogram(prometheus.HistogramOpts{
			Namespace: "rufs",
			Name:      "transfer_read_sizes",
			Help:      "Distribution of read sizes through the VFS",
			Buckets:   bucketsForTransferReadSizes,
		}, nil),
		pb.PushMetricsRequest_TRANSFER_READ_LATENCY: newHistogram(prometheus.HistogramOpts{
			Namespace: "rufs",
			Name:      "transfer_read_latency",
			Help:      "Latency of read RPCs from the VFS",
			Buckets:   bucketsForTransferReadLatency,
		}, []string{"code", "recv_kbytes"}),
		pb.PushMetricsRequest_VFS_FIXED_CONTENT_OPENS: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "vfs_fixed_content_opens",
			Help:      "TODO",
		}, []string{"basename"}),
		pb.PushMetricsRequest_VFS_READDIRS: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "vfs_readdirs",
			Help:      "TODO",
		}, nil),
		pb.PushMetricsRequest_VFS_READDIR_LATENCY: newHistogram(prometheus.HistogramOpts{
			Namespace: "rufs",
			Name:      "vfs_readdir_latency",
			Help:      "TODO",
			Buckets:   bucketsForVfsReaddirLatency,
		}, nil),
		pb.PushMetricsRequest_VFS_PEER_READDIRS: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "vfs_peer_readdirs",
			Help:      "TODO",
		}, []string{"peer", "code"}),
		pb.PushMetricsRequest_VFS_PEER_READDIR_LATENCY: newHistogram(prometheus.HistogramOpts{
			Namespace: "rufs",
			Name:      "vfs_peer_readdir_latency",
			Help:      "TODO",
			Buckets:   bucketsForVfsPeerReaddirLatency,
		}, []string{"peer", "code"}),
		pb.PushMetricsRequest_CONTENT_HASHES: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "content_hashes",
			Help:      "TODO",
		}, nil),
		pb.PushMetricsRequest_CONTENT_RPCS_RECV: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "content_rpcs_recv",
			Help:      "TODO",
		}, []string{"rpc", "caller", "code"}),
		pb.PushMetricsRequest_CONTENT_RPCS_RECV_LATENCY: newHistogram(prometheus.HistogramOpts{
			Namespace: "rufs",
			Name:      "content_rpcs_recv_latency",
			Help:      "TODO",
			Buckets:   bucketsForContentRpcsRecvLatency,
		}, []string{"rpc", "caller", "code"}),
		pb.PushMetricsRequest_CONTENT_ORCHESTRATION_JOINED: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "content_orchestration_joined",
			Help:      "TODO",
		}, []string{"why"}),
		pb.PushMetricsRequest_CONTENT_ORCHESTRATION_JOIN_FAILED: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "content_orchestration_join_failed",
			Help:      "TODO",
		}, []string{"why"}),
		pb.PushMetricsRequest_TRANSFER_RECV_BYTES: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "transfer_recv_bytes",
			Help:      "Number of bytes received from other peers",
		}, []string{"peer", "transfer_type"}),
		pb.PushMetricsRequest_TRANSFER_SEND_BYTES: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "transfer_send_bytes",
			Help:      "Number of bytes received from other peers",
		}, []string{"peer", "transfer_type"}),
	}
)
