// Package intervals provides a set of intervals.
package intervals

import (
	"log"
	"sort"
)

// TODO(quis): The efficiency is painful. Convert it to a fancy tree?

type Interval struct {
	Start, End int64
}

func (i *Interval) Size() int64 {
	return i.End - i.Start
}

type Intervals struct {
	ranges []Interval
}

func (is *Intervals) Add(s, e int64) {
	if s >= e {
		log.Panicf("Invalid call to Intervals.Add(%d, %d)", s, e)
	}
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

func (is *Intervals) AddRange(other Intervals) {
	for _, i := range other.ranges {
		is.Add(i.Start, i.End)
	}
}

func (is *Intervals) Remove(s, e int64) {
	if s >= e {
		log.Panicf("Invalid call to Intervals.Remove(%d, %d)", s, e)
	}
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

func (is *Intervals) RemoveRange(other Intervals) {
	for _, i := range other.ranges {
		is.Remove(i.Start, i.End)
	}
}

func (is *Intervals) Has(s, e int64) bool {
	if s >= e {
		log.Panicf("Invalid call to Intervals.Has(%d, %d)", s, e)
	}
	for _, i := range is.ranges {
		if i.Start <= s && e <= i.End {
			return true
		}
	}
	return false
}

func (is *Intervals) FindUncovered(s, e int64) Intervals {
	if s > e {
		log.Panicf("Invalid call to Intervals.FindUncovered(%d, %d)", s, e)
	} else if s == e {
		return Intervals{}
	}

	res := Intervals{}
	for _, iv := range is.ranges {
		if iv.End <= s {
			// before search range
			continue
		}

		if iv.Start >= e {
			// after search range
			break
		}

		if iv.Start > s {
			// range uncovered since start / last block
			res.Add(s, iv.Start)
		}

		if iv.End >= e {
			// rest of range is covered
			return res
		}

		s = iv.End
	}

	// rest of range is uncovered
	res.Add(s, e)
	return res
}

func (is *Intervals) FindUncoveredRange(other Intervals) Intervals {
	res := Intervals{}
	for _, i := range other.ranges {
		res.AddRange(is.FindUncovered(i.Start, i.End))
	}
	return res
}

func (is *Intervals) Export() []Interval {
	return is.ranges
}

func (is *Intervals) IsEmpty() bool {
	return len(is.ranges) == 0
}

func (is *Intervals) sort() {
	sort.Slice(is.ranges, func(i, j int) bool {
		return is.ranges[i].Start < is.ranges[j].Start
	})
}
