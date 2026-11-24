package telemetry

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
)

func CommunityShortsAlarmTimingForTracking(
	actualPublishedAt *time.Time,
	alarmSentAt *time.Time,
	alarmLatencyMillis *int64,
) alarmtiming.Snapshot {
	timing := alarmtiming.Build(actualPublishedAt, alarmSentAt)
	if timing.AlarmLatencyMillis == nil && alarmLatencyMillis != nil {
		latency := *alarmLatencyMillis

		timing.AlarmLatencyMillis = &latency
	}

	return timing
}
