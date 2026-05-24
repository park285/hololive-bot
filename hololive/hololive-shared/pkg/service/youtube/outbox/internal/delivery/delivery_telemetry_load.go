package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *DeliveryTelemetryRepository) loadTrackingSnapshots(
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
	if err := r.db.WithContext(ctx).
		Where("kind IN ?", kinds).
		Where("content_id IN ?", contentIDs).
		Find(&trackingRows).Error; err != nil {
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

func (r *DeliveryTelemetryRepository) loadObservationWindows(
	ctx context.Context,
	trackingByIdentity map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot,
) ([]domain.YouTubeCommunityShortsObservationWindow, error) {
	if !r.db.Migrator().HasTable(&domain.YouTubeCommunityShortsObservationWindow{}) {
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

func (r *DeliveryTelemetryRepository) queryObservationWindows(
	ctx context.Context,
	earliest time.Time,
	latest time.Time,
) ([]domain.YouTubeCommunityShortsObservationWindow, error) {
	var windows []domain.YouTubeCommunityShortsObservationWindow
	if err := r.db.WithContext(ctx).
		Where("observation_ended_at > ?", earliest).
		Where("observation_started_at <= ?", latest).
		Order("observation_started_at DESC").
		Order("bigbang_cutover_at DESC").
		Find(&windows).Error; err != nil {
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
