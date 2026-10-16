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

package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type DeliveryRepository struct {
	db     deliverysql.DeliveryDB
	logger *slog.Logger
	now    func() time.Time
}

// NewDeliveryRepository의 now는 outbox 집계가 sent로 확정할 때 기록하는 sent_at 시계다.
func NewDeliveryRepository(db any, logger *slog.Logger, now func() time.Time) *DeliveryRepository {
	return &DeliveryRepository{
		db:     AsDeliveryDB(db),
		logger: logger,
		now:    now,
	}
}

func AsDeliveryDB(db any) deliverysql.DeliveryDB {
	if reflectutil.IsNil(db) {
		return nil
	}

	if typed, ok := db.(deliverysql.DeliveryDB); ok {
		return typed
	}

	return nil
}

func (r *DeliveryRepository) UpdateOutboxAggregateStatus(ctx context.Context, outboxID int64) error {
	return r.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID})
}

const outboxAggregateFailedErrorText = "per-room delivery failed"

// 부모를 ID 순서로 잠근 뒤 다음 READ COMMITTED statement에서 자식을 집계한다.
// 잠금 대기 전에 얻은 snapshot으로 집계하면 더 늦게 확정된 SENT를 덮어쓸 수 있다.
func (r *DeliveryRepository) UpdateOutboxAggregateStatuses(ctx context.Context, outboxIDs []int64) error {
	uniqueIDs := deliverysql.UniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	if err := pgx.BeginTxFunc(ctx, r.db, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, mustSQL("delivery_repository_aggregate_lock.sql"), uniqueIDs); err != nil {
			return fmt.Errorf("lock outbox aggregate parents: %w", err)
		}

		if _, err := tx.Exec(ctx, mustSQL("delivery_repository_aggregate_sync.sql"),
			uniqueIDs,
			domain.OutboxStatusPending,
			DeliveryStatusSending,
			domain.OutboxStatusFailed,
			DeliveryStatusQuarantined,
			domain.OutboxStatusSent,
			yttimestamp.Normalize(r.now()),
			outboxAggregateFailedErrorText,
		); err != nil {
			return fmt.Errorf("update outbox aggregate statuses: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("project outbox aggregate transaction: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) FindPendingOutboxIDsForAggregateSync(ctx context.Context, batchSize int) ([]int64, error) {
	if r == nil || r.db == nil || batchSize <= 0 {
		return nil, nil
	}

	var outboxIDs []int64

	if err := dbx.SelectSQL(ctx, r.db, &outboxIDs, "find pending outbox ids for aggregate sync", mustSQL("delivery_repository_0373_10.sql"), batchSize); err != nil {
		return nil, fmt.Errorf("find pending outbox ids for aggregate sync: %w", err)
	}

	return outboxIDs, nil
}
