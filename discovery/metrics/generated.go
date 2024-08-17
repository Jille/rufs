package metrics

// This file is generated by metricgen/gen.go.
import (
	pb "github.com/Jille/rufs/proto"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	metrics = map[pb.PushMetricsRequest_MetricId]processMetric{
		pb.PushMetricsRequest_CLIENT_START_TIME_SECONDS: newGauge(prometheus.GaugeOpts{
			Namespace: "rufs",
			Name:      "client_start_time_seconds",
			Help:      "Timestamp at which each client started",
		}, nil),
		pb.PushMetricsRequest_CLIENT_VERSION: newGauge(prometheus.GaugeOpts{
			Namespace: "rufs",
			Name:      "client_version",
			Help:      "Always 1, the field contains the client version",
		}, []string{"version"}),
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
			Help:      "Number of opens on fixed-content files in the VFS",
		}, []string{"basename"}),
		pb.PushMetricsRequest_VFS_READDIRS: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "vfs_readdirs",
			Help:      "Number of readdir calls on the VFS",
		}, nil),
		pb.PushMetricsRequest_VFS_READDIR_LATENCY: newHistogram(prometheus.HistogramOpts{
			Namespace: "rufs",
			Name:      "vfs_readdir_latency",
			Help:      "Latency of readdir calls on the VFS",
			Buckets:   bucketsForVfsReaddirLatency,
		}, nil),
		pb.PushMetricsRequest_VFS_PEER_READDIRS: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "vfs_peer_readdirs",
			Help:      "Number of readdir RPCs sent to peers from the VFS",
		}, []string{"peer", "code"}),
		pb.PushMetricsRequest_VFS_PEER_READDIR_LATENCY: newHistogram(prometheus.HistogramOpts{
			Namespace: "rufs",
			Name:      "vfs_peer_readdir_latency",
			Help:      "Latency of readdir RPCs sent to peers from the VFS",
			Buckets:   bucketsForVfsPeerReaddirLatency,
		}, []string{"peer", "code"}),
		pb.PushMetricsRequest_CONTENT_HASHES: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "content_hashes",
			Help:      "Number of files we've hashed for this circle",
		}, nil),
		pb.PushMetricsRequest_CONTENT_RPCS_RECV: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "content_rpcs_recv",
			Help:      "Number of RPCs received by the content server",
		}, []string{"rpc", "caller", "code"}),
		pb.PushMetricsRequest_CONTENT_RPCS_RECV_LATENCY: newHistogram(prometheus.HistogramOpts{
			Namespace: "rufs",
			Name:      "content_rpcs_recv_latency",
			Help:      "Latency of incoming RPCs handled by the content server",
			Buckets:   bucketsForContentRpcsRecvLatency,
		}, []string{"rpc", "caller", "code"}),
		pb.PushMetricsRequest_CONTENT_ORCHESTRATION_JOINED: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "content_orchestration_joined",
			Help:      "Number of times we joined an orchestration",
		}, []string{"why"}),
		pb.PushMetricsRequest_CONTENT_ORCHESTRATION_JOIN_FAILED: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "content_orchestration_join_failed",
			Help:      "Number of times we failed to join an orchestration",
		}, []string{"why"}),
		pb.PushMetricsRequest_TRANSFER_RECV_BYTES: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "transfer_recv_bytes",
			Help:      "Number of bytes received from other peers",
		}, []string{"peer", "transfer_type"}),
		pb.PushMetricsRequest_TRANSFER_SEND_BYTES: newCounter(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      "transfer_send_bytes",
			Help:      "Number of bytes sent to other peers",
		}, []string{"peer", "transfer_type"}),
	}
)
