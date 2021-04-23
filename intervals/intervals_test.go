package intervals

import (
	"math/rand"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type dumbIntervals struct {
	items []bool
}

func (di *dumbIntervals) Add(s, e uint64) {
	if uint64(len(di.items)) < e {
		n := make([]bool, e)
		copy(n, di.items)
		di.items = n
	}
	for i := s; e > i; i++ {
		di.items[i] = true
	}
}

func (di *dumbIntervals) Remove(s, e uint64) {
	if uint64(len(di.items)) < e {
		e = uint64(len(di.items))
	}
	for i := s; e > i; i++ {
		di.items[i] = false
	}
}

func (di *dumbIntervals) Export() []Interval {
	var ret []Interval
	var active *Interval
	for i := 0; len(di.items) > i; i++ {
		if di.items[i] {
			if active == nil {
				active = &Interval{Start: uint64(i)}
			}
			active.End = uint64(i) + 1
		} else if active != nil {
			ret = append(ret, *active)
			active = nil
		}
	}
	if active != nil {
		ret = append(ret, *active)
	}
	return ret
}

func TestBasics(t *testing.T) {
	basics(t, &Intervals{})
}

func TestBasicsDumbIntervals(t *testing.T) {
	basics(t, &dumbIntervals{})
}

type someIntervals interface {
	Add(s, e uint64)
	Remove(s, e uint64)
	Export() []Interval
}

func basics(t *testing.T, ivs someIntervals) {
	ivs.Add(10, 20)
	if diff := cmp.Diff([]Interval{Interval{10, 20}}, ivs.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	ivs.Remove(10, 12)
	if diff := cmp.Diff([]Interval{Interval{12, 20}}, ivs.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	ivs.Remove(16, 18)
	if diff := cmp.Diff([]Interval{Interval{12, 16}, Interval{18, 20}}, ivs.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
}

func compare(t *testing.T, di dumbIntervals, si Intervals) {
	t.Helper()
	got := si.Export()
	want := di.Export()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Mismatch: %s", diff)
	}
}

func TestFuzz(t *testing.T) {
	var di dumbIntervals
	var si Intervals
	for i := 0; 10000 > i; i++ {
		s := rand.Intn(1000)
		e := s + rand.Intn(1000-s) + 1
		if rand.Intn(2) == 0 {
			di.Add(uint64(s), uint64(e))
			si.Add(uint64(s), uint64(e))
		} else {
			di.Remove(uint64(s), uint64(e))
			si.Remove(uint64(s), uint64(e))
		}
		compare(t, di, si)
	}
}
