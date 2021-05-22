package metrics

import (
	"strings"
	"sync"

	"github.com/sgielen/rufs/client/config"
	"github.com/sgielen/rufs/common"
	pb "github.com/sgielen/rufs/proto"
)

var (
	circles = map[string]*circleMetrics{}
)

func Init() {
	ReloadConfig()
}

func ReloadConfig() {
	for _, circle := range config.GetCircles() {
		if _, found := circles[circle.Name]; !found {
			circles[circle.Name] = &circleMetrics{}
		}
	}
}

func GetAndResetMetrics(circle string) []*pb.PushMetricsRequest_Metric {
	return circles[circle].GetAndResetMetrics()
}

func setGauge(circs []string, id pb.PushMetricsRequest_MetricId, fields []string, value float64) {
	for _, c := range circs {
		circles[c].SetOrAdd(id, fields, value)
	}
}

func increaseCounter(circs []string, id pb.PushMetricsRequest_MetricId, fields []string, value float64) {
	for _, c := range circs {
		circles[c].SetOrAdd(id, fields, value)
	}
}

func appendDistribution(circs []string, id pb.PushMetricsRequest_MetricId, fields []string, value float64) {
	for _, c := range circs {
		circles[c].SetOrAdd(id, fields, value)
	}
}

func getSingleValue(m pb.PushMetricsRequest_MetricId, v []float64) float64 {
	if isDistributionMetric(m) {
		return 0
	} else {
		return v[0]
	}
}

func getDistributiveValues(m pb.PushMetricsRequest_MetricId, v []float64) []float64 {
	if isDistributionMetric(m) {
		return v
	} else {
		return nil
	}
}

type circleMetrics struct {
	mtx     sync.Mutex
	metrics map[pb.PushMetricsRequest_MetricId]map[string][]float64
}

func (m *circleMetrics) GetAndResetMetrics() []*pb.PushMetricsRequest_Metric {
	res := []*pb.PushMetricsRequest_Metric{}
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.metrics == nil {
		return res
	}
	for id, vs := range m.metrics {
		for fields, v := range vs {
			res = append(res, &pb.PushMetricsRequest_Metric{
				Id:                    id,
				Fields:                common.SplitMaybeEmpty(fields, "\x00"),
				SingleValue:           getSingleValue(id, v),
				NewDistributionValues: getDistributiveValues(id, v),
			})
		}
	}
	m.metrics = nil
	return res
}

func (m *circleMetrics) SetOrAdd(id pb.PushMetricsRequest_MetricId, fields []string, value float64) {
	m.mtx.Lock()
	if m.metrics == nil {
		m.metrics = map[pb.PushMetricsRequest_MetricId]map[string][]float64{}
	}
	if m.metrics[id] == nil {
		m.metrics[id] = map[string][]float64{}
	}
	fs := strings.Join(fields, "\x00")
	if isDistributionMetric(id) {
		m.metrics[id][fs] = append(m.metrics[id][fs], value)
	} else if len(m.metrics[id][fs]) == 0 || !isCounter(id) {
		m.metrics[id][fs] = []float64{value}
	} else if isCounter(id) {
		m.metrics[id][fs][0] += value
	}
	m.mtx.Unlock()
}
