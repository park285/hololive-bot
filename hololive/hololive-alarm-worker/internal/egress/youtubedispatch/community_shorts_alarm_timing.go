package youtubedispatch

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
	timeline "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
)

func appendCommunityShortsAlarmTimingLogAttrs(attrs []any, timing alarmtiming.Snapshot) []any {
	if timing.ActualPublishedAt != nil {
		attrs = append(attrs, slog.Time(FieldActualPublishedAt, timing.ActualPublishedAt.UTC()))
	}

	if timing.AlarmSentAt != nil {
		attrs = append(attrs, slog.Time(FieldAlarmSentAt, timing.AlarmSentAt.UTC()))
	}

	if timing.AlarmLatencyMillis != nil {
		attrs = append(attrs, slog.Int64(FieldAlarmLatencyMillis, *timing.AlarmLatencyMillis))
	}

	if timing.AlarmLatencyExceeded != nil {
		attrs = append(attrs, slog.Bool(FieldAlarmLatencyExceeded, *timing.AlarmLatencyExceeded))
	}

	return attrs
}

func communityShortsAlarmTimingForTimeline(row *timeline.PostDeliveryTimeline) alarmtiming.Snapshot {
	return telemetry.CommunityShortsAlarmTimingForTracking(row.ActualPublishedAt, row.AlarmSentAt, row.AlarmLatencyMillis)
}
