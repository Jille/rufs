// Package intervals provides a set of intervals.
package intervals

import "sort"

// TODO(quis): The efficiency is painful. Convert it to a fancy tree?

type Interval struct {
	Start, End uint64
}

type Intervals struct {
	ranges []Interval
}

func (is *Intervals) Add(s, e uint64) {
	is.Remove(s, e)
	is.ranges = append(is.ranges, Interval{s, e})
	is.sort()
	var newRanges []Interval
	for j, i := range is.ranges {
		if j > 0 && is.ranges[j-1].End == i.Start {
			newRanges[len(newRanges)-1].End = i.End
			continue
		}
		newRanges = append(newRanges, i)
	}
	is.ranges = newRanges
}

func (is *Intervals) Remove(s, e uint64) {
	var newRanges []Interval
	for _, i := range is.ranges {
		if s <= i.Start && i.End <= e {
			continue
		}
		if i.Start < s && e < i.End {
			newRanges = append(newRanges, Interval{i.Start, s}, Interval{e, i.End})
			continue
		}
		if s <= i.Start && i.Start < e {
			i.Start = e
		}
		if s < i.End && i.End <= e {
			i.End = s
		}
		newRanges = append(newRanges, i)
	}
	is.ranges = newRanges
}

func (is *Intervals) Has(s, e uint64) bool {
	for _, i := range is.ranges {
		if i.Start <= s && e <= i.End {
			return true
		}
	}
	return false
}

func (is *Intervals) Export() []Interval {
	return is.ranges
}

func (is *Intervals) sort() {
	sort.Slice(is.ranges, func(i, j int) bool {
		return is.ranges[i].Start < is.ranges[j].Start
	})
}
