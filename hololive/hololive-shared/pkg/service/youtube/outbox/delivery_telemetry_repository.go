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
