package scraper

import (
	"time"
)

var kstLocation = time.FixedZone("KST", 9*60*60)

func calculateNextRunAtHour(now time.Time, hourKST int) time.Time {
	targetHour := hourKST
	if targetHour < 0 {
		targetHour = 0
	}
	if targetHour > 23 {
		targetHour = 23
	}

	nowKST := now.In(kstLocation)
	target := time.Date(
		nowKST.Year(),
		nowKST.Month(),
		nowKST.Day(),
		targetHour,
		0,
		0,
		0,
		kstLocation,
	)
	if !target.After(nowKST) {
		target = target.AddDate(0, 0, 1)
	}
	return target.UTC()
}

func formatKST(value time.Time) string {
	return value.In(kstLocation).Format("2006-01-02 15:04:05 -0700")
}

func buildRetryRuns(baseRun, failedAt time.Time, delays []time.Duration) []time.Time {
	if len(delays) == 0 {
		return nil
	}

	baseKST := baseRun.In(kstLocation)
	failedKST := failedAt.In(kstLocation)

	runs := make([]time.Time, 0, len(delays))
	for _, delay := range delays {
		if delay <= 0 {
			continue
		}

		candidate := baseKST.Add(delay)
		if candidate.Year() != baseKST.Year() || candidate.YearDay() != baseKST.YearDay() {
			continue
		}
		if !candidate.After(failedKST) {
			continue
		}

		runs = append(runs, candidate.UTC())
	}

	return runs
}
