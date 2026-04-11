package outbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
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

func normalizeDeliveryTelemetryObservationStatus(status string) string {
	normalized := strings.TrimSpace(status)
	if normalized == "" {
		return deliveryTelemetryObservationStatusUnclassified
	}
	return normalized
}

func deliveryTelemetryIdentityForRow(row domain.YouTubeNotificationDeliveryTelemetry) (deliveryTelemetryIdentity, bool) {
	kind, ok := deliveryTelemetryKindForAlarmType(row.AlarmType)
	if !ok {
		return deliveryTelemetryIdentity{}, false
	}

	contentID := strings.TrimSpace(row.ContentID)
	if contentID == "" {
		return deliveryTelemetryIdentity{}, false
	}

	return deliveryTelemetryIdentity{kind: kind, contentID: contentID}, true
}

func deliveryTelemetryKindForAlarmType(alarmType domain.AlarmType) (domain.OutboxKind, bool) {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		return domain.OutboxKindCommunityPost, true
	case domain.AlarmTypeShorts:
		return domain.OutboxKindNewShort, true
	default:
		return "", false
	}
}

func (r *DeliveryTelemetryRepository) enrichRows(ctx context.Context, rows []domain.YouTubeNotificationDeliveryTelemetry) error {
	if len(rows) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("enrich delivery telemetry context: db is nil")
	}

	identities := make(map[deliveryTelemetryIdentity]struct{}, len(rows))
	for i := range rows {
		rows[i].ObservationStatus = normalizeDeliveryTelemetryObservationStatus(rows[i].ObservationStatus)
		if identity, ok := deliveryTelemetryIdentityForRow(rows[i]); ok {
			identities[identity] = struct{}{}
		}
	}
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

	return nil
}

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

	var earliest time.Time
	var latest time.Time
	for _, snapshot := range trackingByIdentity {
		if snapshot.actualPublishedAt == nil || snapshot.actualPublishedAt.IsZero() {
			continue
		}
		publishedAt := snapshot.actualPublishedAt.UTC()
		if earliest.IsZero() || publishedAt.Before(earliest) {
			earliest = publishedAt
		}
		if latest.IsZero() || publishedAt.After(latest) {
			latest = publishedAt
		}
	}
	if earliest.IsZero() || latest.IsZero() {
		return nil, nil
	}

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
	row.AlarmLatencyMillis = clonePostLatencyInt64(timing.AlarmLatencyMillis)
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
	row.ActualPublishedAt = cloneUTCTimePtr(timing.ActualPublishedAt)
	row.AlarmSentAt = cloneUTCTimePtr(timing.AlarmSentAt)
	row.AlarmLatencyMillis = clonePostLatencyInt64(timing.AlarmLatencyMillis)
	row.DetectedAt = cloneUTCTimePtr(snapshot.detectedAt)

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
		row.ObservationBigBangCutoverAt = cloneUTCTimePtr(&matchedWindow.BigBangCutoverAt)
		row.ObservationStartedAt = cloneUTCTimePtr(&matchedWindow.ObservationStartedAt)
		row.ObservationEndedAt = cloneUTCTimePtr(&matchedWindow.ObservationEndedAt)
		return
	}

	row.ObservationStatus = deliveryTelemetryObservationStatusOutsideWindow
}

func findMatchingObservationWindow(
	snapshot deliveryTelemetryTrackingSnapshot,
	windows []domain.YouTubeCommunityShortsObservationWindow,
) (domain.YouTubeCommunityShortsObservationWindow, bool) {
	if snapshot.actualPublishedAt == nil || snapshot.actualPublishedAt.IsZero() {
		return domain.YouTubeCommunityShortsObservationWindow{}, false
	}
	if snapshot.detectedAt == nil || snapshot.detectedAt.IsZero() {
		return domain.YouTubeCommunityShortsObservationWindow{}, false
	}

	publishedAt := snapshot.actualPublishedAt.UTC()
	detectedAt := snapshot.detectedAt.UTC()
	for i := range windows {
		window := windows[i]
		if publishedAt.Before(window.ObservationStartedAt.UTC()) {
			continue
		}
		if !publishedAt.Before(window.ObservationEndedAt.UTC()) {
			continue
		}
		if !detectedAt.Before(observationWindowDetectionCutoff(window)) {
			continue
		}
		return window, true
	}
	return domain.YouTubeCommunityShortsObservationWindow{}, false
}

func observationWindowDetectionCutoff(window domain.YouTubeCommunityShortsObservationWindow) time.Time {
	if window.ClosedAt != nil && !window.ClosedAt.IsZero() {
		return window.ClosedAt.UTC()
	}
	return window.ObservationEndedAt.UTC()
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

func deliveryTelemetryObservationContextChanged(
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

	var rows []domain.YouTubeNotificationDeliveryTelemetry
	if err := r.db.WithContext(ctx).
		Where("observation_status = ?", deliveryTelemetryObservationStatusMatched).
		Where("observation_runtime_name = ?", normalizedRuntimeName).
		Where("observation_bigbang_cutover_at = ?", bigBangCutoverAt.UTC()).
		Order("event_at ASC").
		Order("id ASC").
		Find(&rows).Error; err != nil {
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

	var rows []domain.YouTubeNotificationDeliveryTelemetry
	if err := r.db.WithContext(ctx).
		Table("youtube_notification_delivery_telemetry AS t").
		Joins("LEFT JOIN youtube_notification_outbox o ON o.id = t.outbox_id").
		Joins("LEFT JOIN youtube_content_alarm_tracking track ON track.kind = o.kind AND track.content_id = o.content_id").
		Joins("INNER JOIN youtube_community_shorts_observation_post_baselines base ON base.runtime_name = ? AND base.bigbang_cutover_at = ? AND base.kind = track.kind AND base.post_id = track.canonical_content_id", normalizedRuntimeName, bigBangCutoverAt.UTC()).
		Order("t.event_at ASC").
		Order("t.id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list delivery telemetry by finalized observation window: query rows: %w", err)
	}

	return rows, nil
}
