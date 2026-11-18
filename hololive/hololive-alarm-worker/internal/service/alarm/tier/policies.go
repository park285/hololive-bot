package tier

import "time"

func ComputeNextCheckAt(now time.Time, nearestStart *time.Time) time.Time {
	if nearestStart == nil {
		return now.Add(noUpcomingInterval)
	}

	timeToStart := nearestStart.Sub(now)

	return nextCheckAtForTimeToStart(now, timeToStart)
}

func nextCheckAtForTimeToStart(now time.Time, timeToStart time.Duration) time.Time {
	switch {
	case timeToStart <= tier1Window:
		return now.Add(tier1Interval)
	case timeToStart <= tier2Window:
		return now.Add(tier2Interval)
	case timeToStart <= tier3Window:
		return now.Add(tier3Interval)
	default:
		return now.Add(tier4Interval)
	}
}
