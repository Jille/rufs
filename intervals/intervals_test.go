package intervals

import (
	"math/rand"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type dumbIntervals struct {
	items []bool
}

func (di *dumbIntervals) Add(s, e int64) {
	if int64(len(di.items)) < e {
		n := make([]bool, e)
		copy(n, di.items)
		di.items = n
	}
	for i := s; e > i; i++ {
		di.items[i] = true
	}
}

func (di *dumbIntervals) Remove(s, e int64) {
	if int64(len(di.items)) < e {
		e = int64(len(di.items))
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
				active = &Interval{Start: int64(i)}
			}
			active.End = int64(i) + 1
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
	Add(s, e int64)
	Remove(s, e int64)
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
		e := s + rand.Intn(1000-s)
		if rand.Intn(2) == 0 {
			di.Add(int64(s), int64(e))
			si.Add(int64(s), int64(e))
		} else {
			di.Remove(int64(s), int64(e))
			si.Remove(int64(s), int64(e))
		}
		compare(t, di, si)
	}
}

func TestUncovered(t *testing.T) {
	ivs := Intervals{}
	ivs.Add(20, 40)
	ivs.Add(60, 80)
	var uncovered Intervals
	uncovered = ivs.FindUncovered(10, 15)
	if diff := cmp.Diff([]Interval{{10, 15}}, uncovered.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	uncovered = ivs.FindUncovered(10, 20)
	if diff := cmp.Diff([]Interval{{10, 20}}, uncovered.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	uncovered = ivs.FindUncovered(10, 30)
	if diff := cmp.Diff([]Interval{{10, 20}}, uncovered.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	uncovered = ivs.FindUncovered(10, 50)
	if diff := cmp.Diff([]Interval{{10, 20}, {40, 50}}, uncovered.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	uncovered = ivs.FindUncovered(30, 50)
	if diff := cmp.Diff([]Interval{{40, 50}}, uncovered.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	uncovered = ivs.FindUncovered(50, 60)
	if diff := cmp.Diff([]Interval{{50, 60}}, uncovered.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	uncovered = ivs.FindUncovered(50, 70)
	if diff := cmp.Diff([]Interval{{50, 60}}, uncovered.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	uncovered = ivs.FindUncovered(50, 90)
	if diff := cmp.Diff([]Interval{{50, 60}, {80, 90}}, uncovered.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
	uncovered = ivs.FindUncovered(0, 100)
	if diff := cmp.Diff([]Interval{{0, 20}, {40, 60}, {80, 100}}, uncovered.Export()); diff != "" {
		t.Errorf("Mismatch: %s", diff)
	}
}
