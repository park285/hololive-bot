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
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

// outboxPayload: outbox에 저장되는 메시지 payload
type outboxPayload struct {
	Message string `json:"message"`
}

type OutboxRepository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

const deliveryStatusSending domain.DeliveryOutboxStatus = "SENDING"

const staleSendingFailureReason = "stale sending; external send outcome unknown"

const defaultStaleSendingSweepLimit = 100

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

	valueExprs := make([]string, 0, len(items))
	args := make([]any, 0, len(items)*5)
	for i, item := range items {
		payload, err := json.Marshal(outboxPayload{Message: item.Message})
		if err != nil {
			return fmt.Errorf("enqueue batch: marshal payload: %w", err)
		}
		contentID := item.PeriodKey + ":" + item.RoomID
		base := i*5 + 1
		valueExprs = append(valueExprs, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, 'PENDING', 0, NOW())", base, base+1, base+2, base+3, base+4))
		args = append(args, item.Kind, item.PeriodKey, item.RoomID, contentID, string(payload))
	}

	query := mustSQL("outbox_repository_0108_01.sql") + strings.Join(valueExprs, ",") + mustSQL("outbox_repository_0109_02.sql")

	if _, err := r.pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("enqueue batch: %w", err)
	}
	return nil
}

func (r *OutboxRepository) FetchAndLock(ctx context.Context, workerID string, batchSize int, lockTimeout, lease time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
	if err := r.ensurePool(); err != nil {
		return nil, err
	}
	now := time.Now()
	lockExpiry := now.Add(-lockTimeout)
	leaseUntil := now.Add(lease)

	query := mustSQL("outbox_repository_0129_03.sql")

	rows, err := r.pool.Query(ctx, query, lockExpiry, now, batchSize, workerID, leaseUntil)
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
	if lease <= 0 {
		lease = deliveryLease
	}
	now := time.Now()
	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0172_04.sql"),
		deliveryStatusSending, now, now.Add(lease), id, domain.DeliveryStatusPending, workerID,
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
	now := time.Now()
	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0189_05.sql"),
		domain.DeliveryStatusSent, now, id, domain.DeliveryStatusPending, deliveryStatusSending, workerID, lockedAt,
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
	now := time.Now()
	query := mustSQL("outbox_repository_0209_06.sql")

	tag, err := r.pool.Exec(ctx, query, errMsg, maxRetries, now.Add(backoff), id, domain.DeliveryStatusPending, deliveryStatusSending, workerID, now, lockedAt)
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

	now := time.Now()
	_, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0241_07.sql"),
		domain.DeliveryStatusSent, now, ids, domain.DeliveryStatusPending,
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

// FAILED 항목은 sent_at이 NULL이므로 created_at을 fallback으로 사용
func (r *OutboxRepository) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-olderThan)
	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0279_09.sql"),
		domain.DeliveryStatusSent, domain.DeliveryStatusFailed, cutoff,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
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
	cutoff := time.Now().Add(-olderThan)
	tag, err := r.pool.Exec(ctx,
		mustSQL("outbox_repository_0301_10.sql"),
		deliveryStatusSending, cutoff, limit, domain.DeliveryStatusFailed, staleSendingFailureReason,
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
