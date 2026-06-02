package delivery

import (
	"context"
	"fmt"
	"strings"
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

type deliveryTelemetryIdentity struct {
	kind      domain.OutboxKind
	contentID string
}

type deliveryTelemetryTrackingSnapshot struct {
	actualPublishedAt *time.Time
	detectedAt        *time.Time
	alarmSentAt       *time.Time
}

func (r *DeliveryTelemetryRepository) enrichRows(ctx context.Context, rows []domain.YouTubeNotificationDeliveryTelemetry) error {
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
		if identity, ok := deliveryTelemetryIdentityForRow(rows[i]); ok {
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
		identity, ok := deliveryTelemetryIdentityForRow(rows[i])
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

func (r *DeliveryTelemetryRepository) ListByObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list delivery telemetry by observation window: db is nil")
	}

	normalizedRuntimeName := strings.TrimSpace(runtimeName)
	if normalizedRuntimeName == "" {
		return nil, fmt.Errorf("list delivery telemetry by observation window: runtime name is empty")
	}
	if bigBangCutoverAt.IsZero() {
		return nil, fmt.Errorf("list delivery telemetry by observation window: big-bang cutover at is empty")
	}

	rows, err := r.queryTelemetryRows(ctx, "list delivery telemetry by observation window: query rows", `
		SELECT `+deliveryTelemetrySelectColumns()+`
		FROM youtube_notification_delivery_telemetry
		WHERE observation_status = $1
		  AND observation_runtime_name = $2
		  AND observation_bigbang_cutover_at = $3
		ORDER BY event_at ASC, id ASC
	`, deliveryTelemetryObservationStatusMatched, normalizedRuntimeName, bigBangCutoverAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("list delivery telemetry by observation window: query rows: %w", err)
	}

	return rows, nil
}

func (r *DeliveryTelemetryRepository) ListByFinalizedObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list delivery telemetry by finalized observation window: db is nil")
	}

	normalizedRuntimeName := strings.TrimSpace(runtimeName)
	if normalizedRuntimeName == "" {
		return nil, fmt.Errorf("list delivery telemetry by finalized observation window: runtime name is empty")
	}
	if bigBangCutoverAt.IsZero() {
		return nil, fmt.Errorf("list delivery telemetry by finalized observation window: big-bang cutover at is empty")
	}

	rows, err := r.queryTelemetryRows(ctx, "list delivery telemetry by finalized observation window: query rows", `
		SELECT `+deliveryTelemetrySelectColumnsWithAlias("t")+`
		FROM youtube_notification_delivery_telemetry AS t
		LEFT JOIN youtube_notification_outbox o ON o.id = t.outbox_id
		LEFT JOIN youtube_content_alarm_tracking track ON track.kind = o.kind AND track.content_id = o.content_id
		INNER JOIN youtube_community_shorts_observation_post_baselines base
			ON base.runtime_name = $1
			AND base.bigbang_cutover_at = $2
			AND base.kind = track.kind
			AND base.post_id = track.canonical_content_id
		ORDER BY t.event_at ASC, t.id ASC
	`, normalizedRuntimeName, bigBangCutoverAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("list delivery telemetry by finalized observation window: query rows: %w", err)
	}

	return rows, nil
}
