package telemetry

import (
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

func communityShortsAlarmTimingForTelemetryRow(row *domain.YouTubeNotificationDeliveryTelemetry) alarmtiming.Snapshot {
	return CommunityShortsAlarmTimingForTelemetryRow(row)
}

func CommunityShortsAlarmTimingForTelemetryRow(row *domain.YouTubeNotificationDeliveryTelemetry) alarmtiming.Snapshot {
	alarmSentAt := row.AlarmSentAt
	if alarmSentAt == nil && strings.EqualFold(strings.TrimSpace(row.SendResult), "success") && !row.EventAt.IsZero() {
		eventAt := row.EventAt.UTC()
		alarmSentAt = &eventAt
	}
	return CommunityShortsAlarmTimingForTracking(row.ActualPublishedAt, alarmSentAt, row.AlarmLatencyMillis)
}

func CommunityShortsAlarmTimingForTracking(
	actualPublishedAt *time.Time,
	alarmSentAt *time.Time,
	alarmLatencyMillis *int64,
) alarmtiming.Snapshot {
	timing := alarmtiming.Build(actualPublishedAt, alarmSentAt)
	if timing.AlarmLatencyMillis == nil && alarmLatencyMillis != nil {
		timing.AlarmLatencyMillis = timeline.ClonePostLatencyInt64(alarmLatencyMillis)
	}
	return timing
}
