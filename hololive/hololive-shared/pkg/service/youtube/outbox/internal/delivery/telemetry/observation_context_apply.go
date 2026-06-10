package telemetry

import (
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

func applyDeliveryTelemetryObservationContext(
	row *domain.YouTubeNotificationDeliveryTelemetry,
	snapshot *deliveryTelemetryTrackingSnapshot,
	windows []domain.YouTubeCommunityShortsObservationWindow,
) {
	if row == nil {
		return
	}

	timing := communityShortsAlarmTimingForTelemetryRow(*row)
	row.ActualPublishedAt = timing.ActualPublishedAt
	row.AlarmSentAt = timing.AlarmSentAt
	row.AlarmLatencyMillis = timeline.ClonePostLatencyInt64(timing.AlarmLatencyMillis)
	row.DetectedAt = nil
	row.ObservationRuntimeName = ""
	row.ObservationBigBangCutoverAt = nil
	row.ObservationStartedAt = nil
	row.ObservationEndedAt = nil

	if snapshot == nil {
		row.ObservationStatus = deliveryTelemetryObservationStatusTrackingNotFound
		return
	}

	timing = alarmtiming.Build(snapshot.actualPublishedAt, snapshot.alarmSentAt)
	row.ActualPublishedAt = deliverysql.CloneUTCTimePtr(timing.ActualPublishedAt)
	row.AlarmSentAt = deliverysql.CloneUTCTimePtr(timing.AlarmSentAt)
	row.AlarmLatencyMillis = timeline.ClonePostLatencyInt64(timing.AlarmLatencyMillis)
	row.DetectedAt = deliverysql.CloneUTCTimePtr(snapshot.detectedAt)

	if timing.ActualPublishedAt == nil || timing.ActualPublishedAt.IsZero() {
		row.ObservationStatus = deliveryTelemetryObservationStatusMissingActualPublishedAt
		return
	}
	if len(windows) == 0 {
		row.ObservationStatus = deliveryTelemetryObservationStatusWindowNotConfigured
		return
	}

	if matchedWindow, ok := findMatchingObservationWindow(deliveryTelemetryTrackingSnapshot{
		actualPublishedAt: timing.ActualPublishedAt,
		detectedAt:        snapshot.detectedAt,
		alarmSentAt:       snapshot.alarmSentAt,
	}, windows); ok {
		row.ObservationStatus = deliveryTelemetryObservationStatusMatched
		row.ObservationRuntimeName = strings.TrimSpace(matchedWindow.RuntimeName)
		row.ObservationBigBangCutoverAt = deliverysql.CloneUTCTimePtr(&matchedWindow.BigBangCutoverAt)
		row.ObservationStartedAt = deliverysql.CloneUTCTimePtr(&matchedWindow.ObservationStartedAt)
		row.ObservationEndedAt = deliverysql.CloneUTCTimePtr(&matchedWindow.ObservationEndedAt)
		return
	}

	row.ObservationStatus = deliveryTelemetryObservationStatusOutsideWindow
}

func findMatchingObservationWindow(
	snapshot deliveryTelemetryTrackingSnapshot,
	windows []domain.YouTubeCommunityShortsObservationWindow,
) (domain.YouTubeCommunityShortsObservationWindow, bool) {
	publishedAt, detectedAt, ok := snapshotObservationTimes(snapshot)
	if !ok {
		return domain.YouTubeCommunityShortsObservationWindow{}, false
	}

	for i := range windows {
		window := windows[i]
		if !observationWindowMatches(publishedAt, detectedAt, window) {
			continue
		}
		return window, true
	}
	return domain.YouTubeCommunityShortsObservationWindow{}, false
}

func snapshotObservationTimes(snapshot deliveryTelemetryTrackingSnapshot) (time.Time, time.Time, bool) {
	if snapshot.actualPublishedAt == nil || snapshot.actualPublishedAt.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	if snapshot.detectedAt == nil || snapshot.detectedAt.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	return snapshot.actualPublishedAt.UTC(), snapshot.detectedAt.UTC(), true
}

func observationWindowMatches(
	publishedAt time.Time,
	detectedAt time.Time,
	window domain.YouTubeCommunityShortsObservationWindow,
) bool {
	if publishedAt.Before(window.ObservationStartedAt.UTC()) {
		return false
	}
	if !publishedAt.Before(window.ObservationEndedAt.UTC()) {
		return false
	}
	return detectedAt.Before(observationWindowDetectionCutoff(window))
}

func observationWindowDetectionCutoff(window domain.YouTubeCommunityShortsObservationWindow) time.Time {
	if window.ClosedAt != nil && !window.ClosedAt.IsZero() {
		return window.ClosedAt.UTC()
	}
	return window.ObservationEndedAt.UTC()
}
