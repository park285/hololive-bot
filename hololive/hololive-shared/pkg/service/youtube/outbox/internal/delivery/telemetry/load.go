package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

func (r *Repository) loadTrackingSnapshots(
	ctx context.Context,
	identities map[deliveryTelemetryIdentity]struct{},
) (map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot, error) {
	kinds := make([]domain.OutboxKind, 0, len(identities))
	contentIDs := make([]string, 0, len(identities))
	kindSeen := make(map[domain.OutboxKind]struct{}, len(identities))
	contentSeen := make(map[string]struct{}, len(identities))
	for identity := range identities {
		if _, ok := kindSeen[identity.kind]; !ok {
			kindSeen[identity.kind] = struct{}{}
			kinds = append(kinds, identity.kind)
		}
		if _, ok := contentSeen[identity.contentID]; !ok {
			contentSeen[identity.contentID] = struct{}{}
			contentIDs = append(contentIDs, identity.contentID)
		}
	}

	var trackingRows []domain.YouTubeContentAlarmTracking
	if err := deliverysql.SelectDeliverySQL(ctx, r.db, &trackingRows, "enrich delivery telemetry context: load tracking rows", `
		SELECT kind,
			content_id,
			COALESCE(canonical_content_id, '') AS canonical_content_id,
			channel_id,
			actual_published_at,
			detected_at,
			alarm_sent_at,
			alarm_latency_millis,
			alarm_latency_exceeded,
			delivery_status,
			COALESCE(latency_classification_status, '') AS latency_classification_status,
			COALESCE(delay_source, '') AS delay_source,
			COALESCE(internal_delay_cause, '') AS internal_delay_cause,
			created_at,
			updated_at
		FROM youtube_content_alarm_tracking
		WHERE `+deliverysql.DeliveryInClause("kind", len(kinds))+`
		  AND `+deliverysql.DeliveryInClause("content_id", len(contentIDs))+`
	`, deliverysql.AppendDeliveryStringArgs(deliverysql.AppendDeliveryOutboxKindArgs(nil, kinds...), contentIDs)...); err != nil {
		return nil, fmt.Errorf("enrich delivery telemetry context: load tracking rows: %w", err)
	}

	snapshots := make(map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot, len(trackingRows))
	for i := range trackingRows {
		row := trackingRows[i]
		detectedAt := row.DetectedAt.UTC()
		snapshots[deliveryTelemetryIdentity{kind: row.Kind, contentID: strings.TrimSpace(row.ContentID)}] = deliveryTelemetryTrackingSnapshot{
			actualPublishedAt: cloneUTCTimePtr(row.ActualPublishedAt),
			detectedAt:        &detectedAt,
			alarmSentAt:       cloneUTCTimePtr(row.AlarmSentAt),
		}
	}

	return snapshots, nil
}

func (r *Repository) loadObservationWindows(
	ctx context.Context,
	trackingByIdentity map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot,
) ([]domain.YouTubeCommunityShortsObservationWindow, error) {
	var tableExists bool
	if err := r.db.QueryRow(ctx, `SELECT to_regclass('youtube_community_shorts_observation_windows') IS NOT NULL`).Scan(&tableExists); err != nil || !tableExists {
		return nil, nil
	}

	earliest, latest, ok := observationWindowPublishedRange(trackingByIdentity)
	if !ok {
		return nil, nil
	}

	windows, err := r.queryObservationWindows(ctx, earliest, latest)
	if err != nil {
		return nil, err
	}
	return windows, nil
}

func observationWindowPublishedRange(
	trackingByIdentity map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot,
) (time.Time, time.Time, bool) {
	var earliest time.Time
	var latest time.Time
	for _, snapshot := range trackingByIdentity {
		earliest, latest = includePublishedAtInRange(snapshot.actualPublishedAt, earliest, latest)
	}
	return earliest, latest, !earliest.IsZero() && !latest.IsZero()
}

func includePublishedAtInRange(actualPublishedAt *time.Time, earliest time.Time, latest time.Time) (time.Time, time.Time) {
	if actualPublishedAt == nil || actualPublishedAt.IsZero() {
		return earliest, latest
	}
	publishedAt := actualPublishedAt.UTC()
	if earliest.IsZero() || publishedAt.Before(earliest) {
		earliest = publishedAt
	}
	if latest.IsZero() || publishedAt.After(latest) {
		latest = publishedAt
	}
	return earliest, latest
}

func (r *Repository) queryObservationWindows(
	ctx context.Context,
	earliest time.Time,
	latest time.Time,
) ([]domain.YouTubeCommunityShortsObservationWindow, error) {
	var windows []domain.YouTubeCommunityShortsObservationWindow
	if err := deliverysql.SelectDeliverySQL(ctx, r.db, &windows, "enrich delivery telemetry context: load observation windows", `
		SELECT *
		FROM youtube_community_shorts_observation_windows
		WHERE observation_ended_at > ?
		  AND observation_started_at <= ?
		ORDER BY observation_started_at DESC, bigbang_cutover_at DESC
	`, earliest, latest); err != nil {
		if isMissingObservationWindowTableError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("enrich delivery telemetry context: load observation windows: %w", err)
	}

	return windows, nil
}

func isMissingObservationWindowTableError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table: youtube_community_shorts_observation_windows") ||
		strings.Contains(message, `relation "youtube_community_shorts_observation_windows" does not exist`)
}
