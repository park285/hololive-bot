package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	deliveryTelemetryObservationStatusUnclassified             = "unclassified"
	deliveryTelemetryObservationStatusMatched                  = "matched"
	deliveryTelemetryObservationStatusOutsideWindow            = "outside_observation_window"
	deliveryTelemetryObservationStatusMissingActualPublishedAt = "missing_actual_published_at"
	deliveryTelemetryObservationStatusTrackingNotFound         = "tracking_not_found"
	deliveryTelemetryObservationStatusWindowNotConfigured      = "observation_window_not_configured"
)

const (
	ObservationStatusMatched       = deliveryTelemetryObservationStatusMatched
	ObservationStatusOutsideWindow = deliveryTelemetryObservationStatusOutsideWindow
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

	identities := normalizeRowsAndCollectDeliveryTelemetryIdentities(rows)
	if len(identities) == 0 {
		return nil
	}

	trackingByIdentity, err := r.loadTrackingSnapshots(ctx, identities)
	if err != nil {
		return err
	}

	observationWindows, err := r.loadObservationWindows(ctx, trackingByIdentity)
	if err != nil {
		return err
	}

	applyDeliveryTelemetryObservationContexts(rows, trackingByIdentity, observationWindows)
	return nil
}

func normalizeRowsAndCollectDeliveryTelemetryIdentities(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) map[deliveryTelemetryIdentity]struct{} {
	identities := make(map[deliveryTelemetryIdentity]struct{}, len(rows))
	for i := range rows {
		rows[i].ObservationStatus = normalizeDeliveryTelemetryObservationStatus(rows[i].ObservationStatus)
		if identity, ok := deliveryTelemetryIdentityForRow(&rows[i]); ok {
			identities[identity] = struct{}{}
		}
	}
	return identities
}

func applyDeliveryTelemetryObservationContexts(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	trackingByIdentity map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot,
	observationWindows []domain.YouTubeCommunityShortsObservationWindow,
) {
	for i := range rows {
		identity, ok := deliveryTelemetryIdentityForRow(&rows[i])
		if !ok {
			rows[i].ObservationStatus = normalizeDeliveryTelemetryObservationStatus(rows[i].ObservationStatus)
			continue
		}
		snapshot, found := trackingByIdentity[identity]
		if !found {
			applyDeliveryTelemetryObservationContext(&rows[i], nil, observationWindows)
			continue
		}
		snapshotCopy := snapshot
		applyDeliveryTelemetryObservationContext(&rows[i], &snapshotCopy, observationWindows)
	}
}
