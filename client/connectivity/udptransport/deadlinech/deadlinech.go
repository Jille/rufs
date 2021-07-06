package deadlinech

import (
	"sync"
	"time"
)

type DeadlineChannel struct {
	mtx        sync.Mutex
	ch         chan struct{}
	t          *time.Timer
	generation int32
}

func New() *DeadlineChannel {
	return &DeadlineChannel{
		ch: make(chan struct{}),
	}
}

func (d *DeadlineChannel) Set(t time.Time) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	if d.t != nil {
		d.t.Stop()
		d.t = nil
	}
	select {
	case <-d.ch:
		d.ch = make(chan struct{})
	default:
		// Bumping the generation prevents the AfterFunc from closing the channel.
		d.generation++
	}
	if t.IsZero() {
		return
	}
	dur := time.Until(t)
	if dur <= 0 {
		dur = 1
	}
	prevGen := d.generation
	d.t = time.AfterFunc(dur, func() {
		d.mtx.Lock()
		defer d.mtx.Unlock()
		if d.generation == prevGen {
			close(d.ch)
		}
	})
}

func (d *DeadlineChannel) Wait() <-chan struct{} {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	return d.ch
}
