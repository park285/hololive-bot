package dispatch

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/telemetry"
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

func communityShortsAlarmTimingForTimeline(row *PostDeliveryTimeline) alarmtiming.Snapshot {
	return telemetry.CommunityShortsAlarmTimingForTracking(row.ActualPublishedAt, row.AlarmSentAt, row.AlarmLatencyMillis)
}
