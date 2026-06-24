package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

type deliveryTelemetryIdentity struct {
	kind      domain.OutboxKind
	contentID string
}

type deliveryTelemetryTrackingSnapshot struct {
	actualPublishedAt *time.Time
	detectedAt        *time.Time
	alarmSentAt       *time.Time
}

func (r *Repository) enrichRows(ctx context.Context, rows []domain.YouTubeNotificationDeliveryTelemetry) error {
	if len(rows) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("enrich delivery telemetry context: db is nil")
	}

	identities := collectDeliveryTelemetryIdentities(rows)
	if len(identities) == 0 {
		return nil
	}

	trackingByIdentity, err := r.loadTrackingSnapshots(ctx, identities)
	if err != nil {
		return err
	}

	applyDeliveryTelemetryTrackingContexts(rows, trackingByIdentity)
	return nil
}

func collectDeliveryTelemetryIdentities(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) map[deliveryTelemetryIdentity]struct{} {
	identities := make(map[deliveryTelemetryIdentity]struct{}, len(rows))
	for i := range rows {
		if identity, ok := deliveryTelemetryIdentityForRow(&rows[i]); ok {
			identities[identity] = struct{}{}
		}
	}
	return identities
}

func applyDeliveryTelemetryTrackingContexts(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	trackingByIdentity map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot,
) {
	for i := range rows {
		identity, ok := deliveryTelemetryIdentityForRow(&rows[i])
		if !ok {
			continue
		}
		snapshot, found := trackingByIdentity[identity]
		if !found {
			applyDeliveryTelemetryTrackingContext(&rows[i], nil)
			continue
		}
		snapshotCopy := snapshot
		applyDeliveryTelemetryTrackingContext(&rows[i], &snapshotCopy)
	}
}

func applyDeliveryTelemetryTrackingContext(
	row *domain.YouTubeNotificationDeliveryTelemetry,
	snapshot *deliveryTelemetryTrackingSnapshot,
) {
	if row == nil {
		return
	}

	timing := communityShortsAlarmTimingForTelemetryRow(row)
	row.ActualPublishedAt = timing.ActualPublishedAt
	row.AlarmSentAt = timing.AlarmSentAt
	row.AlarmLatencyMillis = timeline.ClonePostLatencyInt64(timing.AlarmLatencyMillis)
	row.DetectedAt = nil

	if snapshot == nil {
		return
	}

	timing = alarmtiming.Build(snapshot.actualPublishedAt, snapshot.alarmSentAt)
	row.ActualPublishedAt = deliverysql.CloneUTCTimePtr(timing.ActualPublishedAt)
	row.AlarmSentAt = deliverysql.CloneUTCTimePtr(timing.AlarmSentAt)
	row.AlarmLatencyMillis = timeline.ClonePostLatencyInt64(timing.AlarmLatencyMillis)
	row.DetectedAt = deliverysql.CloneUTCTimePtr(snapshot.detectedAt)
}

func deliveryTelemetryTrackingContextChanged(
	left *domain.YouTubeNotificationDeliveryTelemetry,
	right *domain.YouTubeNotificationDeliveryTelemetry,
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
