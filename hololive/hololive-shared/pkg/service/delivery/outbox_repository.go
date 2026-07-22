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
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/json"
	"github.com/park285/shared-go/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

// outboxPayload: outbox에 저장되는 메시지 payload
type outboxPayload struct {
	Message string `json:"message"`
}

type outboxBatchRow struct {
	Kind      domain.DeliveryOutboxKind `json:"kind"`
	PeriodKey string                    `json:"period_key"`
	RoomID    string                    `json:"room_id"`
	ContentID string                    `json:"content_id"`
	Payload   outboxPayload             `json:"payload"`
}

type OutboxRepository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

const deliveryStatusSending domain.DeliveryOutboxStatus = "SENDING"

// 결과 불명(stale SENDING)을 FAILED로 회수하면 rearm(outbox_enqueue_batch_upsert.sql의
// WHERE status='FAILED')이 재발송해 중복 노출 위험이 있어 별도 terminal 상태로 격리한다.
const deliveryStatusQuarantined domain.DeliveryOutboxStatus = "QUARANTINED"

const staleSendingFailureReason = "stale sending; external send outcome unknown"

const defaultStaleSendingSweepLimit = 100

const (
	cleanupBatchSize  = 1000
	cleanupBatchYield = 10 * time.Millisecond
)

type OutboxItem struct {
	Kind      domain.DeliveryOutboxKind
	PeriodKey string
	RoomID    string
	Message   string
}

func NewOutboxRepository(postgres database.Client, logger *slog.Logger) *OutboxRepository {
	if postgres == nil {
		return NewOutboxRepositoryFromPool(nil, logger)
	}
	return NewOutboxRepositoryFromPool(postgres.GetPool(), logger)
}

func NewOutboxRepositoryFromPool(pool *pgxpool.Pool, logger *slog.Logger) *OutboxRepository {
	if logger == nil {
		logger = slog.Default()
	}
	return &OutboxRepository{pool: pool, logger: logger}
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
	if err := r.ensurePool(); err != nil {
		return err
	}

	rows := make([]outboxBatchRow, 0, len(items))
	for _, item := range items {
		contentID := item.PeriodKey + ":" + item.RoomID
		rows = append(rows, outboxBatchRow{
			Kind:      item.Kind,
			PeriodKey: item.PeriodKey,
			RoomID:    item.RoomID,
			ContentID: contentID,
			Payload:   outboxPayload{Message: item.Message},
		})
	}

	raw, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("enqueue batch: marshal rows: %w", err)
	}

	if _, err := r.pool.Exec(ctx, mustSQL("outbox_enqueue_batch_upsert.sql"), string(raw)); err != nil {
		return fmt.Errorf("enqueue batch: %w", err)
	}
	return nil
}

func (r *OutboxRepository) FetchAndLock(ctx context.Context, workerID string, batchSize int, lockTimeout, lease time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
	if err := r.ensurePool(); err != nil {
		return nil, err
	}
	query := mustSQL("outbox_repository_0129_03.sql")
	rows, err := r.pool.Query(ctx, query,
		positiveDurationMilliseconds(lockTimeout),
		batchSize,
		workerID,
		positiveDurationMilliseconds(lease),
	)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock: %w", err)
	}
	defer rows.Close()

	items, err := pgx.CollectRows(rows, scanNotificationDeliveryOutbox)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock: %w", err)
	}
	return items, nil
}

func (r *OutboxRepository) MarkSending(ctx context.Context, id int64, workerID string, lease time.Duration) (bool, error) {
	if err := r.ensurePool(); err != nil {
		return false, err
	}
	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0172_04.sql"),
		deliveryStatusSending, positiveDurationMilliseconds(lease),
		id, domain.DeliveryStatusPending, workerID,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *OutboxRepository) MarkSent(ctx context.Context, id int64, workerID string, lockedAt time.Time) (bool, error) {
	if err := r.ensurePool(); err != nil {
		return false, err
	}
	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0189_05.sql"),
		domain.DeliveryStatusSent, id, domain.DeliveryStatusPending, deliveryStatusSending, workerID, lockedAt,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *OutboxRepository) MarkFailed(ctx context.Context, id int64, workerID string, lockedAt time.Time, maxRetries int, backoff time.Duration, errMsg string) (bool, error) {
	if err := r.ensurePool(); err != nil {
		return false, err
	}
	query := mustSQL("outbox_repository_0209_06.sql")

	tag, err := r.pool.Exec(ctx, query,
		errMsg, maxRetries, durationMilliseconds(backoff), id,
		domain.DeliveryStatusPending, deliveryStatusSending, workerID, lockedAt,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *OutboxRepository) MarkSentBatch(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if err := r.ensurePool(); err != nil {
		return err
	}

	_, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0241_07.sql"),
		domain.DeliveryStatusSent, ids, domain.DeliveryStatusPending,
	)
	if err != nil {
		return fmt.Errorf("mark sent batch: %w", err)
	}
	return nil
}

func (r *OutboxRepository) MarkFailedBatch(ctx context.Context, ids []int64, reason string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := r.ensurePool(); err != nil {
		return err
	}

	_, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0261_08.sql"),
		domain.DeliveryStatusFailed, reason, ids, domain.DeliveryStatusPending,
	)
	if err != nil {
		return fmt.Errorf("mark failed batch: %w", err)
	}
	return nil
}

// FAILED/QUARANTINED 항목은 sent_at이 NULL이므로 created_at을 fallback으로 사용
func (r *OutboxRepository) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	return r.cleanupInBatches(ctx, time.Now().Add(-olderThan), cleanupBatchSize)
}

func (r *OutboxRepository) cleanupInBatches(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, err
	}
	var total int64
	for {
		tag, err := r.pool.Exec(ctx,
			mustSQL("outbox_repository_0279_09.sql"),
			domain.DeliveryStatusSent, domain.DeliveryStatusFailed, deliveryStatusQuarantined, cutoff, batchSize,
		)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < int64(batchSize) {
			return total, nil
		}
		if err := yieldBetweenCleanupBatches(ctx); err != nil {
			return total, err
		}
	}
}

func yieldBetweenCleanupBatches(ctx context.Context) error {
	if retry.Sleep(ctx, cleanupBatchYield) {
		return nil
	}
	return ctx.Err()
}

func (r *OutboxRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = defaultStaleSendingSweepLimit
	}
	if olderThan <= 0 {
		olderThan = deliveryLease
	}
	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0301_10.sql"),
		deliveryStatusSending, positiveDurationMilliseconds(olderThan),
		limit, deliveryStatusQuarantined, staleSendingFailureReason,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *OutboxRepository) CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, err
	}
	var count int64
	err := r.pool.QueryRow(ctx, mustSQL("outbox_repository_0330_11.sql"), status).Scan(&count)
	return count, err
}

func durationMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	if milliseconds := value.Milliseconds(); milliseconds > 0 {
		return milliseconds
	}
	return 1
}

func positiveDurationMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		value = deliveryLease
	}
	if milliseconds := durationMilliseconds(value); milliseconds > 0 {
		return milliseconds
	}
	return 1
}

func (r *OutboxRepository) ensurePool() error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("notification delivery outbox repository: postgres pool is required")
	}
	return nil
}

func scanNotificationDeliveryOutbox(row pgx.CollectableRow) (domain.NotificationDeliveryOutbox, error) {
	var item domain.NotificationDeliveryOutbox
	var kind string
	var status string
	var payload []byte
	var lockedAt sql.NullTime
	var sentAt sql.NullTime
	var errText sql.NullString
	err := row.Scan(
		&item.ID,
		&kind,
		&item.PeriodKey,
		&item.RoomID,
		&item.ContentID,
		&payload,
		&status,
		&item.AttemptCount,
		&item.NextAttemptAt,
		&item.CreatedAt,
		&lockedAt,
		&sentAt,
		&errText,
	)
	if err != nil {
		return domain.NotificationDeliveryOutbox{}, err
	}
	item.Kind = domain.DeliveryOutboxKind(kind)
	item.Payload = string(payload)
	item.Status = domain.DeliveryOutboxStatus(status)
	item.LockedAt = lockedAt
	item.SentAt = sentAt
	item.Error = errText
	return item, nil
}
