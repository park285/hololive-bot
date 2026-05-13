package outbox

import (
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func sameUTCTimePtr(left, right *time.Time) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.UTC().Equal(right.UTC())
	}
}

func sameInt64Ptr(left, right *int64) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return *left == *right
	}
}

func deliveryTelemetryObservationContextChanged(
	left domain.YouTubeNotificationDeliveryTelemetry,
	right domain.YouTubeNotificationDeliveryTelemetry,
) bool {
	return deliveryTelemetryObservationTimingChanged(left, right) ||
		deliveryTelemetryObservationWindowChanged(left, right)
}

func deliveryTelemetryObservationTimingChanged(
	left domain.YouTubeNotificationDeliveryTelemetry,
	right domain.YouTubeNotificationDeliveryTelemetry,
) bool {
	if !sameUTCTimePtr(left.ActualPublishedAt, right.ActualPublishedAt) {
		return true
	}
	if !sameUTCTimePtr(left.AlarmSentAt, right.AlarmSentAt) {
		return true
	}
	if !sameInt64Ptr(left.AlarmLatencyMillis, right.AlarmLatencyMillis) {
		return true
	}
	if !sameUTCTimePtr(left.DetectedAt, right.DetectedAt) {
		return true
	}
	return false
}

func deliveryTelemetryObservationWindowChanged(
	left domain.YouTubeNotificationDeliveryTelemetry,
	right domain.YouTubeNotificationDeliveryTelemetry,
) bool {
	if normalizeDeliveryTelemetryObservationStatus(left.ObservationStatus) != normalizeDeliveryTelemetryObservationStatus(right.ObservationStatus) {
		return true
	}
	if strings.TrimSpace(left.ObservationRuntimeName) != strings.TrimSpace(right.ObservationRuntimeName) {
		return true
	}
	if !sameUTCTimePtr(left.ObservationBigBangCutoverAt, right.ObservationBigBangCutoverAt) {
		return true
	}
	if !sameUTCTimePtr(left.ObservationStartedAt, right.ObservationStartedAt) {
		return true
	}
	if !sameUTCTimePtr(left.ObservationEndedAt, right.ObservationEndedAt) {
		return true
	}
	return false
}
