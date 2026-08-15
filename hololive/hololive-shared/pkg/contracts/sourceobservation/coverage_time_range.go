package sourceobservation

import "time"

type coverageTimeRange struct {
	start *time.Time
	end   *time.Time
}

func filterTimeRange(filters VideoListFiltersV1) coverageTimeRange {
	return coverageTimeRange{start: filters.PublishedAfter, end: filters.PublishedBefore}
}

func timeRangesEqual(left, right coverageTimeRange) bool {
	return sameOptionalTime(left.start, right.start) && sameOptionalTime(left.end, right.end)
}

func timeRangesOverlap(left, right coverageTimeRange) bool {
	if left.end != nil && right.start != nil && left.end.Before(*right.start) {
		return false
	}
	if right.end != nil && left.start != nil && right.end.Before(*left.start) {
		return false
	}
	return true
}

func timeRangeContains(outer, inner coverageTimeRange) bool {
	if outer.start != nil && (inner.start == nil || inner.start.Before(*outer.start)) {
		return false
	}
	if outer.end != nil && (inner.end == nil || inner.end.After(*outer.end)) {
		return false
	}
	return true
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
