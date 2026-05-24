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
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/json"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// outboxPayload: outbox에 저장되는 메시지 payload
type outboxPayload struct {
	Message string `json:"message"`
}

type OutboxRepository struct {
	db     *gorm.DB
	logger *slog.Logger
}

type OutboxItem struct {
	Kind      domain.DeliveryOutboxKind
	PeriodKey string
	RoomID    string
	Message   string
}

func NewOutboxRepository(db *gorm.DB, logger *slog.Logger) *OutboxRepository {
	return &OutboxRepository{db: db, logger: logger}
}

func (r *OutboxRepository) Enqueue(ctx context.Context, kind domain.DeliveryOutboxKind, periodKey, roomID, message string) error {
	return r.EnqueueBatch(ctx, []OutboxItem{
		{
			Kind:      kind,
			PeriodKey: periodKey,
			RoomID:    roomID,
			Message:   message,
		},
	})
}

func (r *OutboxRepository) EnqueueBatch(ctx context.Context, items []OutboxItem) error {
	if len(items) == 0 {
		return nil
	}

	valueExprs := make([]string, 0, len(items))
	args := make([]any, 0, len(items)*5)
	for _, item := range items {
		payload, err := json.Marshal(outboxPayload{Message: item.Message})
		if err != nil {
			return fmt.Errorf("enqueue batch: marshal payload: %w", err)
		}
		contentID := item.PeriodKey + ":" + item.RoomID
		valueExprs = append(valueExprs, "(?, ?, ?, ?, ?, 'PENDING', 0, NOW())")
		args = append(args, item.Kind, item.PeriodKey, item.RoomID, contentID, string(payload))
	}

	sql := `INSERT INTO notification_delivery_outbox (kind, period_key, room_id, content_id, payload, status, attempt_count, next_attempt_at)
		VALUES ` + strings.Join(valueExprs, ",") + `
		ON CONFLICT (kind, content_id) DO UPDATE
		SET payload = EXCLUDED.payload, status = 'PENDING', attempt_count = 0, next_attempt_at = NOW(), error = NULL
		WHERE notification_delivery_outbox.status = 'FAILED'`

	result := r.db.WithContext(ctx).Exec(sql, args...)
	if result.Error != nil {
		return fmt.Errorf("enqueue batch: %w", result.Error)
	}
	return nil
}

func (r *OutboxRepository) FetchAndLock(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
	lockExpiry := time.Now().Add(-lockTimeout)
	now := time.Now()

	var items []domain.NotificationDeliveryOutbox
	sql := `WITH claim AS (
        SELECT id FROM notification_delivery_outbox
        WHERE status = 'PENDING'
          AND (locked_at IS NULL OR locked_at < ?)
          AND next_attempt_at <= ?
        ORDER BY created_at ASC LIMIT ?
        FOR UPDATE SKIP LOCKED
    )
    UPDATE notification_delivery_outbox o SET locked_at = ?
    FROM claim WHERE o.id = claim.id
    RETURNING o.*`

	err := r.db.WithContext(ctx).Raw(sql, lockExpiry, now, batchSize, now).Scan(&items).Error
	if err != nil {
		return nil, fmt.Errorf("fetch and lock: %w", err)
	}
	return items, nil
}

func (r *OutboxRepository) MarkSent(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Model(&domain.NotificationDeliveryOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":  domain.DeliveryStatusSent,
			"sent_at": time.Now(),
			"error":   nil,
		}).Error
}

func (r *OutboxRepository) MarkFailed(ctx context.Context, id int64, maxRetries int, backoff time.Duration, errMsg string) error {
	now := time.Now()
	sql := `UPDATE notification_delivery_outbox
            SET attempt_count = attempt_count + 1,
                error = ?,
                status = CASE WHEN attempt_count + 1 >= ? THEN 'FAILED' ELSE 'PENDING' END,
                next_attempt_at = CASE WHEN attempt_count + 1 >= ? THEN next_attempt_at ELSE ? END,
                locked_at = NULL
            WHERE id = ?`

	return r.db.WithContext(ctx).Exec(sql, errMsg, maxRetries, maxRetries, now.Add(backoff), id).Error
}

func (r *OutboxRepository) MarkSentBatch(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&domain.NotificationDeliveryOutbox{}).
		Where("id IN ? AND status = ?", ids, domain.DeliveryStatusPending).
		Updates(map[string]any{
			"status":    domain.DeliveryStatusSent,
			"sent_at":   now,
			"locked_at": nil,
			"error":     nil,
		})
	if result.Error != nil {
		return fmt.Errorf("mark sent batch: %w", result.Error)
	}
	return nil
}

func (r *OutboxRepository) MarkFailedBatch(ctx context.Context, ids []int64, reason string) error {
	if len(ids) == 0 {
		return nil
	}

	result := r.db.WithContext(ctx).
		Model(&domain.NotificationDeliveryOutbox{}).
		Where("id IN ?", ids).
		Updates(map[string]any{
			"status":    domain.DeliveryStatusFailed,
			"error":     reason,
			"locked_at": nil,
		})
	if result.Error != nil {
		return fmt.Errorf("mark failed batch: %w", result.Error)
	}
	return nil
}

// FAILED 항목은 sent_at이 NULL이므로 created_at을 fallback으로 사용
func (r *OutboxRepository) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result := r.db.WithContext(ctx).
		Where("status IN (?, ?) AND COALESCE(sent_at, created_at) < ?", domain.DeliveryStatusSent, domain.DeliveryStatusFailed, cutoff).
		Delete(&domain.NotificationDeliveryOutbox{})
	return result.RowsAffected, result.Error
}

func (r *OutboxRepository) CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.NotificationDeliveryOutbox{}).
		Where("status = ?", status).Count(&count).Error
	return count, err
}
