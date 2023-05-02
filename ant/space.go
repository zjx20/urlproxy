package ant

import "fmt"

var zeroRange = dataRange{}

type dataRange struct {
	begin int64
	end   int64
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

type space struct {
	covered []dataRange
}

func newSpace() *space {
	return &space{}
}

func (s *space) cover(r dataRange) {
	if r.begin > r.end {
		panic(fmt.Sprintf("invalid range: %+v", r))
	}
	pos := 0
	for pos = 0; pos < len(s.covered); pos++ {
		c := &s.covered[pos]
		if c.begin > r.end {
			// -------------[cccccccc]------
			//    [rrrrrr]
			break
		}
		if c.begin <= r.end && c.end > r.end {
			if c.begin > r.begin {
				// ---------[cccccccc]------
				//     [rrrrrr]
				c.begin = r.begin
				return
			} else {
				// -----[cccccccc]------
				//       [rrrrrr]
				return
			}
		}
		if c.end >= r.begin {
			if c.begin <= r.begin {
				// -----[cccccccc]------
				//            [rrrrrr]
				c.end = r.end
				return
			} else {
				// -----[cccccccc]------
				//     [rrrrrrrrrr]
				*c = r
				return
			}
		}

		// -----[cccccc]----------
		//               [rrrrr]
		continue
	}

	s.covered = append(s.covered, dataRange{})
	copy(s.covered[pos+1:], s.covered[pos:])
	s.covered[pos] = r
}

func (s *space) coveredRange(begin int64) dataRange {
	c := dataRange{}
	for i := range s.covered {
		if s.covered[i].begin <= c.end {
			c.end = max(c.end, s.covered[i].end)
		} else {
			if c.begin <= begin && c.end > begin {
				return dataRange{
					begin: max(c.begin, begin),
					end:   c.end,
				}
			}
			c = s.covered[i]
		}
	}
	if c.begin <= begin && c.end > begin {
		return dataRange{
			begin: max(c.begin, begin),
			end:   c.end,
		}
	}
	return zeroRange
}

func (s *space) isCovered(pos int64) bool {
	for i := range s.covered {
		if s.covered[i].begin <= pos && s.covered[i].end > pos {
			return true
		}
	}
	return false
}
