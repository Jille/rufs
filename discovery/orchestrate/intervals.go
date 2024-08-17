package orchestrate

import (
	pb "github.com/Jille/rufs/proto"
)

type info struct {
	PeersHave      []string
	PeersReadNow   []string
	PeersReadAhead []string
	PeersReceiving []string
}

func (i *info) Clone() info {
	res := info{
		PeersHave:      append([]string{}, i.PeersHave...),
		PeersReadNow:   append([]string{}, i.PeersReadNow...),
		PeersReadAhead: append([]string{}, i.PeersReadAhead...),
		PeersReceiving: append([]string{}, i.PeersReceiving...),
	}
	return res
}

func (i *info) PeerIsReceiving(peer string) bool {
	for _, p := range i.PeersReceiving {
		if p == peer {
			return true
		}
	}
	return false
}

type range_info struct {
	Start, End int64
	Data       info
}

type range_infos struct {
	ranges []*range_info
}

func (rs *range_infos) splitRangeAt(i int, r int64) {
	if rs.ranges[i].Start >= r || rs.ranges[i].End <= r {
		panic("splitRangeAt(), but r isn't fully inside i")
	}

	new_range := &range_info{
		Start: r,
		End:   rs.ranges[i].End,
		Data:  rs.ranges[i].Data.Clone(),
	}
	rs.ranges[i].End = r

	// Make space for the new element
	rs.ranges = append(rs.ranges, &range_info{})

	// Copy elements after the split element
	copy(rs.ranges[i+2:], rs.ranges[i+1:])
	rs.ranges[i+1] = new_range
}

func (rs *range_infos) GetSliceFromRange(r *pb.Range) []*range_info {
	return rs.GetSlice(r.Start, r.End)
}

func (rs *range_infos) GetSlice(start, end int64) []*range_info {
	// Make sure that rs.ranges fully overlaps with the slice requested
	if len(rs.ranges) == 0 {
		rs.ranges = []*range_info{{start, end, info{}}}
	}
	if rs.ranges[0].Start > start {
		rs.ranges = append([]*range_info{{start, rs.ranges[0].Start, info{}}}, rs.ranges...)
	}
	if rs.ranges[len(rs.ranges)-1].End < end {
		rs.ranges = append(rs.ranges, &range_info{rs.ranges[len(rs.ranges)-1].End, end, info{}})
	}

	// Find (or create) the last range that ends with $end
	firstRangeAfter := -1
	for i := len(rs.ranges) - 1; i >= 0; i = i - 1 {
		if rs.ranges[i].Start < end {
			if rs.ranges[i].End > end {
				// Partial match, split this range into two
				rs.splitRangeAt(i, end)
			}
			firstRangeAfter = i + 1
			break
		}
	}

	if firstRangeAfter < 0 || firstRangeAfter > len(rs.ranges) || rs.ranges[firstRangeAfter-1].End != end {
		panic("Invalid firstRangeAfter found")
	}

	// Find (or create) the first range that starts with $start
	firstRange := -1
	for i := firstRangeAfter - 1; i >= 0; i = i - 1 {
		if rs.ranges[i].Start <= start {
			if rs.ranges[i].Start < start {
				// Partial match, split this range into two
				rs.splitRangeAt(i, start)
				firstRangeAfter = firstRangeAfter + 1
				i = i + 1
			}
			firstRange = i
			break
		}
	}

	if firstRange < 0 || firstRange >= firstRangeAfter || rs.ranges[firstRange].Start != start {
		panic("Invalid firstRange found")
	}

	return rs.ranges[firstRange:firstRangeAfter]
}
