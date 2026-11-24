package youtubedispatch

import (
	"log/slog"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/telemetry"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
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

func finalizedCommunityShortsAlarmTiming(result *store.TerminalCommunityShortsOutboxResult) alarmtiming.Snapshot {
	if !result.Tracked {
		return alarmtiming.Build(nil, result.SentAt)
	}

	return telemetry.CommunityShortsAlarmTimingForTracking(result.ActualPublishedAt, result.AlarmSentAt, result.AlarmLatencyMillis)
}
