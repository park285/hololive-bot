package timeutil

import (
	"math"
	"time"
)

var KSTZone = time.FixedZone("KST", 9*60*60)

func FirstNonNilTime(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			return value
		}
	}

	return nil
}

func ToKST(t time.Time) time.Time {
	return t.In(KSTZone)
}

func FormatKST(t time.Time, layout string) string {
	return t.In(KSTZone).Format(layout)
}

func NowKST() time.Time {
	return time.Now().In(KSTZone)
}

func MinutesUntilFloorPtr(target *time.Time, reference time.Time) int {
	if target == nil {
		return -1
	}

	if target.Before(reference) {
		return -1
	}

	duration := target.Sub(reference)
	minutesUntil := math.Floor(duration.Minutes())

	if minutesUntil < 0 {
		return -1
	}

	return int(minutesUntil)
}

func MinutesUntilCeilPtr(target *time.Time, reference time.Time) int {
	if target == nil || !target.After(reference) {
		return -1
	}

	duration := target.Sub(reference)

	return int((duration + time.Minute - 1) / time.Minute)
}
