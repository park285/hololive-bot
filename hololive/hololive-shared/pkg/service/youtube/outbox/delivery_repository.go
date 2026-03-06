package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// DeliveryRepository: room 단위 전달 상태 저장소
type DeliveryRepository struct {
	db     *gorm.DB
	logger *slog.Logger
}

type deliveryStatusCount struct {
	OutboxID int64
	Status   domain.OutboxStatus
	Count    int64
}

func NewDeliveryRepository(db *gorm.DB, logger *slog.Logger) *DeliveryRepository {
	return &DeliveryRepository{
		db:     db,
		logger: logger,
	}
}

// EnqueueBatch: outbox 이벤트를 room 단위 delivery row로 fan-out 저장한다.
func (r *DeliveryRepository) EnqueueBatch(ctx context.Context, outboxID int64, roomIDs []string) error {
	uniqueRoomIDs := uniqueStrings(roomIDs)
	if len(uniqueRoomIDs) == 0 {
		return nil
	}

	now := time.Now()
	rows := make([]domain.YouTubeNotificationDelivery, 0, len(uniqueRoomIDs))
	for _, roomID := range uniqueRoomIDs {
		rows = append(rows, domain.YouTubeNotificationDelivery{
			OutboxID:      outboxID,
			RoomID:        roomID,
			Status:        domain.OutboxStatusPending,
			AttemptCount:  0,
			NextAttemptAt: now,
		})
	}

	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "outbox_id"}, {Name: "room_id"}},
		DoNothing: true,
	}).Create(&rows)
	if result.Error != nil {
		return fmt.Errorf("enqueue delivery batch: create rows: %w", result.Error)
	}

	return nil
}

// FetchAndLock: PENDING delivery를 배치 claim하고 locked_at을 갱신한다.
func (r *DeliveryRepository) FetchAndLock(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.YouTubeNotificationDelivery, error) {
	lockExpiry := time.Now().Add(-lockTimeout)
	now := time.Now()

	var rows []domain.YouTubeNotificationDelivery
	err := r.db.WithContext(ctx).Raw(`
		WITH claim AS (
			SELECT id
			FROM youtube_notification_delivery
			WHERE status = ?
			  AND (locked_at IS NULL OR locked_at < ?)
			  AND next_attempt_at <= ?
			ORDER BY created_at ASC
			LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		UPDATE youtube_notification_delivery d
		SET locked_at = ?
		FROM claim
		WHERE d.id = claim.id
		RETURNING d.id, d.outbox_id, d.room_id, d.status, d.attempt_count,
		          d.next_attempt_at, d.created_at, d.locked_at, d.sent_at, d.error
	`, domain.OutboxStatusPending, lockExpiry, now, batchSize, now).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("fetch and lock delivery rows: %w", err)
	}

	return rows, nil
}

// MarkSentBatch: 전달 성공 row를 배치로 SENT 처리한다.
func (r *DeliveryRepository) MarkSentBatch(ctx context.Context, ids []int64) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	result := r.db.WithContext(ctx).Model(&domain.YouTubeNotificationDelivery{}).
		Where("id IN ? AND status = ?", uniqueIDs, domain.OutboxStatusPending).
		Updates(map[string]any{
			"status":    domain.OutboxStatusSent,
			"sent_at":   time.Now(),
			"locked_at": nil,
			"error":     "",
		})
	if result.Error != nil {
		return fmt.Errorf("mark delivery rows sent: %w", result.Error)
	}

	return nil
}

// MarkFailed: 전달 실패 row를 retry 또는 FAILED로 전환한다.
func (r *DeliveryRepository) MarkFailed(ctx context.Context, id int64, maxRetries int, backoff time.Duration, errMsg string) error {
	now := time.Now()
	nextAttempt := now.Add(backoff)

	err := r.db.WithContext(ctx).Exec(`
		UPDATE youtube_notification_delivery
		SET attempt_count = attempt_count + 1,
		    error = ?,
		    status = CASE WHEN attempt_count + 1 >= ? THEN ? ELSE ? END,
		    next_attempt_at = CASE WHEN attempt_count + 1 >= ? THEN next_attempt_at ELSE ? END,
		    locked_at = NULL
		WHERE id = ?
	`, truncateString(errMsg, 500), maxRetries, domain.OutboxStatusFailed, domain.OutboxStatusPending, maxRetries, nextAttempt, id).Error
	if err != nil {
		return fmt.Errorf("mark delivery row failed: %w", err)
	}

	return nil
}

// MarkFailedRetryBatch: 전달 실패 row들을 retry-aware semantics로 배치 갱신한다.
func (r *DeliveryRepository) MarkFailedRetryBatch(ctx context.Context, ids []int64, maxRetries int, backoff time.Duration, errMsg string) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	now := time.Now()
	nextAttempt := now.Add(backoff)

	err := r.db.WithContext(ctx).Exec(`
		UPDATE youtube_notification_delivery
		SET attempt_count = attempt_count + 1,
		    error = ?,
		    status = CASE WHEN attempt_count + 1 >= ? THEN ? ELSE ? END,
		    next_attempt_at = CASE WHEN attempt_count + 1 >= ? THEN next_attempt_at ELSE ? END,
		    locked_at = NULL
		WHERE id IN ?
	`, truncateString(errMsg, 500), maxRetries, domain.OutboxStatusFailed, domain.OutboxStatusPending, maxRetries, nextAttempt, uniqueIDs).Error
	if err != nil {
		return fmt.Errorf("mark delivery rows failed batch: %w", err)
	}

	return nil
}

// UpdateOutboxAggregateStatus: delivery 상태를 집계해 outbox 상태를 갱신한다.
func (r *DeliveryRepository) UpdateOutboxAggregateStatus(ctx context.Context, outboxID int64) error {
	return r.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID})
}

// UpdateOutboxAggregateStatuses: 여러 outbox의 delivery 상태를 한 번에 집계해 갱신한다.
func (r *DeliveryRepository) UpdateOutboxAggregateStatuses(ctx context.Context, outboxIDs []int64) error {
	uniqueIDs := uniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	var counts []deliveryStatusCount
	if err := r.db.WithContext(ctx).
		Model(&domain.YouTubeNotificationDelivery{}).
		Select("outbox_id, status, COUNT(*) AS count").
		Where("outbox_id IN ?", uniqueIDs).
		Group("outbox_id, status").
		Scan(&counts).Error; err != nil {
		return fmt.Errorf("update outbox aggregate statuses: count delivery statuses: %w", err)
	}

	statusGroups := groupOutboxIDsByAggregateStatus(uniqueIDs, counts)
	for status, ids := range statusGroups {
		if err := r.updateOutboxStatusBatch(ctx, ids, status); err != nil {
			return err
		}
	}

	return nil
}

func (r *DeliveryRepository) updateOutboxStatusBatch(ctx context.Context, outboxIDs []int64, status domain.OutboxStatus) error {
	uniqueIDs := uniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	updates := map[string]any{
		"status":    status,
		"locked_at": nil,
	}
	switch status {
	case domain.OutboxStatusSent:
		updates["sent_at"] = time.Now()
		updates["error"] = ""
	case domain.OutboxStatusFailed:
		updates["error"] = "per-room delivery failed"
	}

	result := r.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id IN ?", uniqueIDs).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update outbox aggregate statuses: apply update: %w", result.Error)
	}

	return nil
}

func groupOutboxIDsByAggregateStatus(outboxIDs []int64, counts []deliveryStatusCount) map[domain.OutboxStatus][]int64 {
	perOutboxCounts := make(map[int64][]deliveryStatusCount, len(outboxIDs))
	for _, item := range counts {
		perOutboxCounts[item.OutboxID] = append(perOutboxCounts[item.OutboxID], item)
	}

	grouped := make(map[domain.OutboxStatus][]int64, 3)
	for _, outboxID := range outboxIDs {
		pendingCount, sentCount, failedCount := parseStatusCounts(perOutboxCounts[outboxID])
		status := resolveOutboxStatus(pendingCount, sentCount, failedCount)
		grouped[status] = append(grouped[status], outboxID)
	}

	return grouped
}

func parseStatusCounts(counts []deliveryStatusCount) (pending int64, sent int64, failed int64) {
	for _, item := range counts {
		switch item.Status {
		case domain.OutboxStatusPending:
			pending = item.Count
		case domain.OutboxStatusSent:
			sent = item.Count
		case domain.OutboxStatusFailed:
			failed = item.Count
		}
	}
	return pending, sent, failed
}

func resolveOutboxStatus(pending int64, sent int64, failed int64) domain.OutboxStatus {
	switch {
	case pending > 0:
		return domain.OutboxStatusPending
	case failed > 0:
		return domain.OutboxStatusFailed
	case sent > 0:
		return domain.OutboxStatusSent
	default:
		return domain.OutboxStatusPending
	}
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}
