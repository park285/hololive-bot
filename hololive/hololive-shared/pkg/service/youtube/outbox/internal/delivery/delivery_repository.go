// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type DeliveryRepository struct {
	db     *gorm.DB
	logger *slog.Logger
}

type deliveryStatusCount struct {
	OutboxID int64
	Status   domain.OutboxStatus
	Count    int64
}

type deliveryAlarmSentTarget struct {
	Kind      domain.OutboxKind `gorm:"column:kind"`
	ContentID string            `gorm:"column:content_id"`
}

func NewDeliveryRepository(db *gorm.DB, logger *slog.Logger) *DeliveryRepository {
	return &DeliveryRepository{
		db:     db,
		logger: logger,
	}
}

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

func (r *DeliveryRepository) FetchAndLock(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.YouTubeNotificationDelivery, error) {
	if r == nil || r.db == nil || batchSize <= 0 {
		return nil, nil
	}
	if r.db.Dialector != nil && r.db.Name() == "sqlite" {
		return r.fetchAndLockSQLite(ctx, batchSize, lockTimeout)
	}

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

func (r *DeliveryRepository) fetchAndLockSQLite(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.YouTubeNotificationDelivery, error) {
	lockExpiry := time.Now().Add(-lockTimeout)
	now := time.Now()

	var rows []domain.YouTubeNotificationDelivery
	if err := r.db.WithContext(ctx).
		Where("status = ?", domain.OutboxStatusPending).
		Where("(locked_at IS NULL OR locked_at < ?) AND next_attempt_at <= ?", lockExpiry, now).
		Order("created_at ASC").
		Limit(batchSize).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("fetch and lock delivery rows (sqlite): %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	ids := collectDeliveryIDs(rows)
	if err := r.db.WithContext(ctx).
		Model(&domain.YouTubeNotificationDelivery{}).
		Where("id IN ?", ids).
		Update("locked_at", now).Error; err != nil {
		return nil, fmt.Errorf("lock delivery rows (sqlite): %w", err)
	}

	for i := range rows {
		lockedAt := now
		rows[i].LockedAt = &lockedAt
	}

	return rows, nil
}

func (r *DeliveryRepository) MarkSentBatch(ctx context.Context, ids []int64, claimTokens ...deliveryClaimToken) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("mark delivery rows sent: db is nil")
	}

	sentAt := canonicalSentAtNow()
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return markSentBatchTx(ctx, tx, uniqueIDs, sentAt, claimTokens)
	}); err != nil {
		return fmt.Errorf("mark delivery rows sent transaction: %w", err)
	}

	return nil
}

func markSentBatchTx(
	ctx context.Context,
	tx *gorm.DB,
	uniqueIDs []int64,
	sentAt time.Time,
	claimTokens []deliveryClaimToken,
) error {
	trackingMarks, err := loadAlarmSentMarksForPendingDeliveryIDs(ctx, tx, uniqueIDs, sentAt, claimTokens)
	if err != nil {
		return fmt.Errorf("load tracking marks: %w", err)
	}
	if err := updateSentDeliveryRows(tx, uniqueIDs, sentAt); err != nil {
		return err
	}
	return persistSentDeliveryTracking(ctx, tx, trackingMarks)
}

func updateSentDeliveryRows(tx *gorm.DB, uniqueIDs []int64, sentAt time.Time) error {
	result := tx.Model(&domain.YouTubeNotificationDelivery{}).
		Where("id IN ? AND status = ?", uniqueIDs, domain.OutboxStatusPending).
		Updates(map[string]any{
			"status":    domain.OutboxStatusSent,
			"sent_at":   sentAt,
			"locked_at": nil,
			"error":     "",
		})
	if result.Error != nil {
		return fmt.Errorf("update delivery rows: %w", result.Error)
	}
	return nil
}

func persistSentDeliveryTracking(
	ctx context.Context,
	tx *gorm.DB,
	trackingMarks []trackingrepo.AlarmSentMark,
) error {
	if err := trackingrepo.NewRepository(tx).MarkAlarmSentBatch(ctx, trackingMarks); err != nil {
		return fmt.Errorf("update tracking rows: %w", err)
	}
	return persistSentDeliveryLatencyClassifications(ctx, tx, trackingMarks)
}

func persistSentDeliveryLatencyClassifications(
	ctx context.Context,
	tx *gorm.DB,
	trackingMarks []trackingrepo.AlarmSentMark,
) error {
	if len(trackingMarks) == 0 {
		return nil
	}
	identities := make([]PostTrackingIdentity, 0, len(trackingMarks))
	for i := range trackingMarks {
		identities = append(identities, PostTrackingIdentity{Kind: trackingMarks[i].Kind, ContentID: trackingMarks[i].ContentID})
	}
	if err := NewDeliveryTelemetryRepository(tx).PersistPostLatencyClassificationsByIdentities(ctx, identities); err != nil {
		return fmt.Errorf("persist tracking latency classifications: %w", err)
	}
	return nil
}

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

func (r *DeliveryRepository) MarkPermanentFailureBatch(ctx context.Context, ids []int64, maxRetries int, errMsg string) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	err := r.db.WithContext(ctx).Exec(`
		UPDATE youtube_notification_delivery
		SET attempt_count = CASE WHEN attempt_count >= ? THEN attempt_count ELSE ? END,
		    error = ?,
		    status = ?,
		    locked_at = NULL
		WHERE id IN ? AND status = ?
	`, maxRetries, maxRetries, truncateString(errMsg, 500), domain.OutboxStatusFailed, uniqueIDs, domain.OutboxStatusPending).Error
	if err != nil {
		return fmt.Errorf("mark delivery rows permanent failed batch: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) UpdateOutboxAggregateStatus(ctx context.Context, outboxID int64) error {
	return r.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID})
}

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
	sentAt := canonicalSentAtNow()
	switch status {
	case domain.OutboxStatusSent:
		updates["sent_at"] = sentAt
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

func (r *DeliveryRepository) FindPendingOutboxIDsForAggregateSync(ctx context.Context, batchSize int) ([]int64, error) {
	if r == nil || r.db == nil || batchSize <= 0 {
		return nil, nil
	}

	var outboxIDs []int64
	if err := r.db.WithContext(ctx).Raw(`
		SELECT d.outbox_id
		FROM youtube_notification_delivery d
		JOIN youtube_notification_outbox o ON o.id = d.outbox_id
		WHERE o.status = ?
		GROUP BY d.outbox_id
		HAVING SUM(CASE WHEN d.status = ? THEN 1 ELSE 0 END) = 0
		ORDER BY d.outbox_id ASC
		LIMIT ?
	`, domain.OutboxStatusPending, domain.OutboxStatusPending, batchSize).Scan(&outboxIDs).Error; err != nil {
		return nil, fmt.Errorf("find pending outbox ids for aggregate sync: %w", err)
	}

	return outboxIDs, nil
}
