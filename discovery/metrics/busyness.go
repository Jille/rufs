package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type BusynessMetric struct {
	busyMetric  *prometheus.CounterVec
	totalMetric *prometheus.CounterVec
}

type BusynessSubMetric struct {
	busyMetric  prometheus.Counter
	totalMetric prometheus.Counter

	lastUpdate time.Time
	busy       bool
}

func NewBusynessMetric(name string, labels []string) *BusynessMetric {
	return &BusynessMetric{
		busyMetric: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      name + "_busy_seconds",
			Help:      "Number of seconds " + name + " was busy",
		}, labels),
		totalMetric: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "rufs",
			Name:      name + "_total_seconds",
			Help:      "Number of seconds " + name + " existed (busy+idle)",
		}, labels),
	}
}

func (b *BusynessMetric) Instance(labelValues []string) *BusynessSubMetric {
	return &BusynessSubMetric{
		busyMetric:  b.busyMetric.WithLabelValues(labelValues...),
		totalMetric: b.totalMetric.WithLabelValues(labelValues...),
	}
}

func (b *BusynessSubMetric) Busy() {
	now := time.Now()
	if b.lastUpdate.IsZero() {
		b.lastUpdate = now
	} else if b.busy {
		panic("two consecutive BusynessMetric.Busy() calls")
	}
	b.totalMetric.Add(now.Sub(b.lastUpdate).Seconds())
	b.lastUpdate = now
	b.busy = true
}

func (b *BusynessSubMetric) Idle() {
	now := time.Now()
	if b.lastUpdate.IsZero() {
		b.lastUpdate = now
		return
	} else if !b.busy {
		panic("two consecutive BusynessMetric.Idle() calls")
	}
	d := now.Sub(b.lastUpdate).Seconds()
	b.busyMetric.Add(d)
	b.totalMetric.Add(d)
	b.lastUpdate = now
	b.busy = false
}
