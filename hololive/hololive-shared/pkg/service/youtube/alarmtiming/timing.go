package alarmtiming

import (
	"time"

	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

const LatencyExceededThresholdMillis = int64((2 * time.Minute) / time.Millisecond)

type Snapshot struct {
	ActualPublishedAt    *time.Time
	AlarmSentAt          *time.Time
	AlarmLatencyMillis   *int64
	AlarmLatencyExceeded *bool
}

func Build(actualPublishedAt *time.Time, alarmSentAt *time.Time) Snapshot {
	normalizedPublishedAt := yttimestamp.NormalizePtr(actualPublishedAt)
	normalizedAlarmSentAt := yttimestamp.NormalizePtr(alarmSentAt)
	latencyMillis, latencyExceeded := CalculateLatency(normalizedPublishedAt, normalizedAlarmSentAt)

	return Snapshot{
		ActualPublishedAt:    normalizedPublishedAt,
		AlarmSentAt:          normalizedAlarmSentAt,
		AlarmLatencyMillis:   latencyMillis,
		AlarmLatencyExceeded: latencyExceeded,
	}
}

func CalculateLatency(actualPublishedAt *time.Time, alarmSentAt *time.Time) (*int64, *bool) {
	if actualPublishedAt == nil || alarmSentAt == nil {
		return nil, nil
	}

	latencyMillis := alarmSentAt.UTC().Sub(actualPublishedAt.UTC()).Milliseconds()
	exceeded := latencyMillis > LatencyExceededThresholdMillis
	return &latencyMillis, &exceeded
}
