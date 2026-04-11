package outbox

import (
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
)

func appendCommunityShortsAlarmTimingLogAttrs(attrs []any, timing alarmtiming.Snapshot) []any {
	if timing.ActualPublishedAt != nil {
		attrs = append(attrs, slog.Time(logschema.FieldActualPublishedAt, timing.ActualPublishedAt.UTC()))
	}
	if timing.AlarmSentAt != nil {
		attrs = append(attrs, slog.Time(logschema.FieldAlarmSentAt, timing.AlarmSentAt.UTC()))
	}
	if timing.AlarmLatencyMillis != nil {
		attrs = append(attrs, slog.Int64(logschema.FieldAlarmLatencyMillis, *timing.AlarmLatencyMillis))
	}
	if timing.AlarmLatencyExceeded != nil {
		attrs = append(attrs, slog.Bool(logschema.FieldAlarmLatencyExceeded, *timing.AlarmLatencyExceeded))
	}
	return attrs
}

func communityShortsAlarmTimingForTelemetryRow(row domain.YouTubeNotificationDeliveryTelemetry) alarmtiming.Snapshot {
	alarmSentAt := row.AlarmSentAt
	if alarmSentAt == nil && strings.EqualFold(strings.TrimSpace(row.SendResult), "success") && !row.EventAt.IsZero() {
		eventAt := row.EventAt.UTC()
		alarmSentAt = &eventAt
	}
	return communityShortsAlarmTimingForTracking(row.ActualPublishedAt, alarmSentAt, row.AlarmLatencyMillis)
}

func communityShortsAlarmTimingForTimeline(row PostDeliveryTimeline) alarmtiming.Snapshot {
	return communityShortsAlarmTimingForTracking(row.ActualPublishedAt, row.AlarmSentAt, row.AlarmLatencyMillis)
}

func communityShortsAlarmTimingForTracking(
	actualPublishedAt *time.Time,
	alarmSentAt *time.Time,
	alarmLatencyMillis *int64,
) alarmtiming.Snapshot {
	timing := alarmtiming.Build(actualPublishedAt, alarmSentAt)
	if timing.AlarmLatencyMillis == nil && alarmLatencyMillis != nil {
		timing.AlarmLatencyMillis = clonePostLatencyInt64(alarmLatencyMillis)
	}
	return timing
}
