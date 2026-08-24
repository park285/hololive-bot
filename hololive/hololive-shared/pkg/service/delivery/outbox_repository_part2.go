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
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func yieldBetweenCleanupBatches(ctx context.Context) error {
	if retry.Sleep(ctx, cleanupBatchYield) {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("yield between cleanup batches: %w", err)
	}

	return nil
}

func (r *OutboxRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, fmt.Errorf("ensure pool: %w", err)
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
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (r *OutboxRepository) CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error) {
	if err := r.ensurePool(); err != nil {
		return 0, fmt.Errorf("ensure pool: %w", err)
	}

	var count int64

	if err := r.pool.QueryRow(ctx, mustSQL("outbox_repository_0330_11.sql"), status).Scan(&count); err != nil {
		return 0, fmt.Errorf("count by status: %w", err)
	}

	return count, nil
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
		return errors.New("notification delivery outbox repository: postgres pool is required")
	}

	return nil
}

func scanNotificationDeliveryOutbox(row pgx.CollectableRow) (domain.NotificationDeliveryOutbox, error) {
	var (
		item     domain.NotificationDeliveryOutbox
		kind     string
		status   string
		payload  []byte
		lockedAt sql.NullTime
		sentAt   sql.NullTime
		errText  sql.NullString
	)

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
		return domain.NotificationDeliveryOutbox{}, fmt.Errorf("scan: %w", err)
	}

	item.Kind = domain.DeliveryOutboxKind(kind)
	item.Payload = string(payload)
	item.Status = domain.DeliveryOutboxStatus(status)
	item.LockedAt = lockedAt
	item.SentAt = sentAt
	item.Error = errText

	return item, nil
}
