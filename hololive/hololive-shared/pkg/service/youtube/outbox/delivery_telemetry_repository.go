package outbox

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type DeliveryTelemetryRepository struct {
	db *gorm.DB
}

func NewDeliveryTelemetryRepository(db *gorm.DB) *DeliveryTelemetryRepository {
	return &DeliveryTelemetryRepository{db: db}
}

func cloneUTCTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}

	normalized := value.UTC()
	return &normalized
}

func (r *DeliveryTelemetryRepository) Enqueue(ctx context.Context, rows []domain.YouTubeNotificationDeliveryTelemetry) error {
	prepared, err := r.prepareRows(ctx, rows)
	if err != nil {
		return err
	}
	return r.enqueuePrepared(ctx, prepared)
}

func (r *DeliveryTelemetryRepository) prepareRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	normalized := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, len(rows))
	now := time.Now().UTC()
	for i := range rows {
		if row, ok := prepareDeliveryTelemetryRow(rows[i], now); ok {
			normalized = append(normalized, row)
		}
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	if err := r.enrichRows(ctx, normalized); err != nil {
		return nil, err
	}

	return normalized, nil
}

func prepareDeliveryTelemetryRow(
	row domain.YouTubeNotificationDeliveryTelemetry,
	now time.Time,
) (domain.YouTubeNotificationDeliveryTelemetry, bool) {
	if row.DeliveryID <= 0 || row.AttemptOrdinal <= 0 {
		return domain.YouTubeNotificationDeliveryTelemetry{}, false
	}

	normalizeDeliveryTelemetryAttemptTimes(&row, now)
	applyDeliveryTelemetryTiming(&row)
	applyDeliveryTelemetryDefaults(&row, now)
	return row, true
}

func normalizeDeliveryTelemetryAttemptTimes(row *domain.YouTubeNotificationDeliveryTelemetry, now time.Time) {
	row.AttemptStartedAt = cloneUTCTimePtr(row.AttemptStartedAt)
	row.AttemptFinishedAt = cloneUTCTimePtr(row.AttemptFinishedAt)
	if row.AttemptFinishedAt == nil && !row.EventAt.IsZero() {
		finishedAt := row.EventAt.UTC()
		row.AttemptFinishedAt = &finishedAt
	}
	if row.EventAt.IsZero() && row.AttemptFinishedAt != nil {
		row.EventAt = row.AttemptFinishedAt.UTC()
	}
	if row.EventAt.IsZero() {
		row.EventAt = now
	}
	if row.AttemptFinishedAt == nil {
		finishedAt := row.EventAt.UTC()
		row.AttemptFinishedAt = &finishedAt
	}
}

func applyDeliveryTelemetryTiming(row *domain.YouTubeNotificationDeliveryTelemetry) {
	timing := communityShortsAlarmTimingForTelemetryRow(*row)
	row.ActualPublishedAt = timing.ActualPublishedAt
	row.AlarmSentAt = timing.AlarmSentAt
	row.AlarmLatencyMillis = clonePostLatencyInt64(timing.AlarmLatencyMillis)
}

func applyDeliveryTelemetryDefaults(row *domain.YouTubeNotificationDeliveryTelemetry, now time.Time) {
	if row.NextAttemptAt.IsZero() {
		row.NextAttemptAt = now
	}
	row.DeliveryPath = normalizeCommunityShortsDeliveryPath(row.DeliveryPath)
	applyTelemetryPostID(row)
	row.ObservationStatus = normalizeDeliveryTelemetryObservationStatus(row.ObservationStatus)
}

func (r *DeliveryTelemetryRepository) enqueuePrepared(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) error {
	if len(rows) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("enqueue delivery telemetry: db is nil")
	}

	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "delivery_id"}, {Name: "attempt_ordinal"}},
		DoNothing: true,
	}).Create(&rows).Error; err != nil {
		return fmt.Errorf("enqueue delivery telemetry: %w", err)
	}

	return nil
}

func (r *DeliveryTelemetryRepository) FetchAndLockPending(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if batchSize <= 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	lockExpiry := now.Add(-lockTimeout)

	var candidates []domain.YouTubeNotificationDeliveryTelemetry
	if err := r.db.WithContext(ctx).
		Where("logged_at IS NULL").
		Where("next_attempt_at <= ?", now).
		Where("locked_at IS NULL OR locked_at < ?", lockExpiry).
		Order("event_at ASC").
		Limit(batchSize).
		Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("fetch pending delivery telemetry: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	candidateIDs := make([]int64, 0, len(candidates))
	for i := range candidates {
		candidateIDs = append(candidateIDs, candidates[i].ID)
	}
	slices.Sort(candidateIDs)

	if err := r.db.WithContext(ctx).
		Model(&domain.YouTubeNotificationDeliveryTelemetry{}).
		Where("id IN ?", candidateIDs).
		Where("logged_at IS NULL").
		Where("locked_at IS NULL OR locked_at < ?", lockExpiry).
		Updates(map[string]any{"locked_at": now}).Error; err != nil {
		return nil, fmt.Errorf("lock delivery telemetry rows: %w", err)
	}

	var locked []domain.YouTubeNotificationDeliveryTelemetry
	if err := r.db.WithContext(ctx).
		Where("id IN ?", candidateIDs).
		Where("locked_at = ?", now).
		Order("event_at ASC").
		Find(&locked).Error; err != nil {
		return nil, fmt.Errorf("reload locked delivery telemetry rows: %w", err)
	}
	if err := r.refreshLockedRows(ctx, locked); err != nil {
		return nil, err
	}

	return locked, nil
}

func (r *DeliveryTelemetryRepository) MarkLoggedBatch(ctx context.Context, ids []int64) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	now := time.Now().UTC()
	if err := r.db.WithContext(ctx).
		Model(&domain.YouTubeNotificationDeliveryTelemetry{}).
		Where("id IN ?", uniqueIDs).
		Updates(map[string]any{
			"logged_at": now,
			"locked_at": nil,
			"error":     "",
		}).Error; err != nil {
		return fmt.Errorf("mark delivery telemetry logged: %w", err)
	}

	return nil
}

func (r *DeliveryTelemetryRepository) MarkRetryBatch(ctx context.Context, ids []int64, backoff time.Duration, errMsg string) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	nextAttemptAt := time.Now().UTC().Add(backoff)
	if err := r.db.WithContext(ctx).
		Model(&domain.YouTubeNotificationDeliveryTelemetry{}).
		Where("id IN ?", uniqueIDs).
		Updates(map[string]any{
			"locked_at":       nil,
			"next_attempt_at": nextAttemptAt,
			"error":           truncateString(errMsg, 500),
		}).Error; err != nil {
		return fmt.Errorf("mark delivery telemetry retry: %w", err)
	}

	return nil
}

func (r *DeliveryTelemetryRepository) refreshLockedRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
) error {
	if len(rows) == 0 {
		return nil
	}

	enriched := append([]domain.YouTubeNotificationDeliveryTelemetry(nil), rows...)
	if err := r.enrichRows(ctx, enriched); err != nil {
		return fmt.Errorf("refresh locked delivery telemetry rows: %w", err)
	}

	for i := range enriched {
		if !deliveryTelemetryObservationContextChanged(rows[i], enriched[i]) {
			rows[i] = enriched[i]
			continue
		}

		if err := r.db.WithContext(ctx).
			Model(&domain.YouTubeNotificationDeliveryTelemetry{}).
			Where("id = ?", enriched[i].ID).
			Updates(map[string]any{
				"actual_published_at":            enriched[i].ActualPublishedAt,
				"alarm_sent_at":                  enriched[i].AlarmSentAt,
				"alarm_latency_millis":           enriched[i].AlarmLatencyMillis,
				"detected_at":                    enriched[i].DetectedAt,
				"observation_status":             normalizeDeliveryTelemetryObservationStatus(enriched[i].ObservationStatus),
				"observation_runtime_name":       strings.TrimSpace(enriched[i].ObservationRuntimeName),
				"observation_bigbang_cutover_at": enriched[i].ObservationBigBangCutoverAt,
				"observation_started_at":         enriched[i].ObservationStartedAt,
				"observation_ended_at":           enriched[i].ObservationEndedAt,
			}).Error; err != nil {
			return fmt.Errorf("refresh locked delivery telemetry rows: update row %d: %w", enriched[i].ID, err)
		}
		rows[i] = enriched[i]
	}

	return nil
}

type deliveryTelemetryBackfillCandidate struct {
	DeliveryID        int64
	OutboxID          int64
	RoomID            string
	Status            domain.OutboxStatus
	AttemptCount      int
	DeliveryError     string
	DeliverySentAt    *time.Time
	DeliveryLockedAt  *time.Time
	DeliveryCreatedAt time.Time
	Kind              domain.OutboxKind
	ChannelID         string
	ContentID         string
	Payload           string
}

func (r *DeliveryTelemetryRepository) BackfillFromDelivery(ctx context.Context, limit int, since time.Time) (int, error) {
	if limit <= 0 {
		return 0, nil
	}

	candidates, err := r.loadBackfillCandidates(ctx, limit, since)
	if err != nil {
		return 0, err
	}
	events := buildBackfillEvents(candidates)
	if len(events) == 0 {
		return 0, nil
	}
	if err := r.Enqueue(ctx, events); err != nil {
		return 0, err
	}
	if err := r.PersistPostLatencyClassificationsByOutboxIDs(ctx, collectTelemetryOutboxIDs(events)); err != nil {
		return 0, fmt.Errorf("persist backfilled post latency classifications: %w", err)
	}

	return len(events), nil
}

func (r *DeliveryTelemetryRepository) loadBackfillCandidates(
	ctx context.Context,
	limit int,
	since time.Time,
) ([]deliveryTelemetryBackfillCandidate, error) {
	var candidates []deliveryTelemetryBackfillCandidate
	query := r.db.WithContext(ctx).
		Table("youtube_notification_delivery AS d").
		Select(strings.Join([]string{
			"d.id AS delivery_id",
			"d.outbox_id AS outbox_id",
			"d.room_id AS room_id",
			"d.status AS status",
			"d.attempt_count AS attempt_count",
			"d.error AS delivery_error",
			"d.sent_at AS delivery_sent_at",
			"d.locked_at AS delivery_locked_at",
			"d.created_at AS delivery_created_at",
			"o.kind AS kind",
			"o.channel_id AS channel_id",
			"o.content_id AS content_id",
			"o.payload AS payload",
		}, ", ")).
		Joins("JOIN youtube_notification_outbox o ON o.id = d.outbox_id").
		Where("o.kind IN ?", []domain.OutboxKind{domain.OutboxKindNewShort, domain.OutboxKindCommunityPost}).
		Where(`
			(d.status = ? AND d.sent_at IS NOT NULL)
			OR (d.status IN (?, ?) AND d.attempt_count > 0 AND COALESCE(d.error, '') <> '')
		`, domain.OutboxStatusSent, domain.OutboxStatusPending, domain.OutboxStatusFailed)
	if !since.IsZero() {
		query = query.Where("COALESCE(d.sent_at, d.locked_at, d.created_at) >= ?", since.UTC())
	}
	query = query.Order("COALESCE(d.sent_at, d.locked_at, d.created_at) ASC").
		Limit(limit)
	if err := query.Scan(&candidates).Error; err != nil {
		return nil, fmt.Errorf("backfill delivery telemetry candidates: %w", err)
	}

	return candidates, nil
}

func buildBackfillEvents(candidates []deliveryTelemetryBackfillCandidate) []domain.YouTubeNotificationDeliveryTelemetry {
	events := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, len(candidates))
	for i := range candidates {
		event, ok := buildBackfillEvent(candidates[i])
		if !ok {
			continue
		}
		events = append(events, *event)
	}

	return events
}

func buildBackfillEvent(candidate deliveryTelemetryBackfillCandidate) (*domain.YouTubeNotificationDeliveryTelemetry, bool) {
	attemptOrdinal, sendResult, failureReason := backfillAttemptMetadata(candidate)
	if attemptOrdinal <= 0 {
		return nil, false
	}

	eventAt := backfillCandidateEventAt(candidate)
	dedupeKey, dedupeErr := domain.BuildYouTubeNotificationDedupeKey(candidate.Kind, candidate.ContentID)
	if dedupeErr != nil {
		dedupeKey = dedupeKeyLogValue(domain.YouTubeNotificationOutbox{Kind: candidate.Kind, ContentID: candidate.ContentID})
	}
	attemptStartedAt := cloneUTCTimePtr(candidate.DeliveryLockedAt)
	attemptFinishedAt := eventAt

	return &domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:        candidate.DeliveryID,
		AttemptOrdinal:    attemptOrdinal,
		OutboxID:          candidate.OutboxID,
		ChannelID:         candidate.ChannelID,
		ContentID:         candidate.ContentID,
		PostID:            resolveTelemetryPostID(candidate.Kind, candidate.ContentID, candidate.Payload),
		RoomID:            candidate.RoomID,
		AlarmType:         candidate.Kind.ToAlarmType(),
		DedupeKey:         dedupeKey,
		DeliveryPath:      communityShortsDeliveryPath,
		DeliveryMode:      "recovered",
		SendResult:        sendResult,
		FailureReason:     truncateString(failureReason, 100),
		AttemptStartedAt:  attemptStartedAt,
		AttemptFinishedAt: &attemptFinishedAt,
		EventAt:           eventAt,
		NextAttemptAt:     time.Now().UTC(),
	}, true
}

func backfillAttemptMetadata(candidate deliveryTelemetryBackfillCandidate) (int, string, string) {
	attemptOrdinal := candidate.AttemptCount
	sendResult := "failure"
	failureReason := strings.TrimSpace(candidate.DeliveryError)
	if candidate.Status == domain.OutboxStatusSent {
		attemptOrdinal = candidate.AttemptCount + 1
		sendResult = "success"
		failureReason = ""
	}

	return attemptOrdinal, sendResult, failureReason
}

func backfillCandidateEventAt(candidate deliveryTelemetryBackfillCandidate) time.Time {
	eventAt := candidate.DeliveryCreatedAt.UTC()
	if candidate.DeliverySentAt != nil {
		return candidate.DeliverySentAt.UTC()
	}
	if candidate.DeliveryLockedAt != nil {
		return candidate.DeliveryLockedAt.UTC()
	}
	return eventAt
}

func (r *DeliveryTelemetryRepository) DeleteLoggedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if r == nil || r.db == nil || cutoff.IsZero() {
		return 0, nil
	}

	result := r.db.WithContext(ctx).
		Where("logged_at IS NOT NULL").
		Where("event_at < ?", cutoff.UTC()).
		Delete(&domain.YouTubeNotificationDeliveryTelemetry{})
	if result.Error != nil {
		return 0, fmt.Errorf("delete delivery telemetry before cutoff: %w", result.Error)
	}

	return result.RowsAffected, nil
}

func collectTelemetryOutboxIDs(rows []domain.YouTubeNotificationDeliveryTelemetry) []int64 {
	outboxIDs := make([]int64, 0, len(rows))
	for i := range rows {
		if rows[i].OutboxID <= 0 {
			continue
		}
		outboxIDs = append(outboxIDs, rows[i].OutboxID)
	}
	return uniqueInt64s(outboxIDs)
}
