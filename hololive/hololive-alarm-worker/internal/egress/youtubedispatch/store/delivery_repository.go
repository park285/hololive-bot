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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type DeliveryRepository struct {
	db     deliverysql.DeliveryDB
	logger *slog.Logger
}

func NewDeliveryRepository(db any, logger *slog.Logger) *DeliveryRepository {
	return &DeliveryRepository{
		db:     AsDeliveryDB(db),
		logger: logger,
	}
}

func AsDeliveryDB(db any) deliverysql.DeliveryDB {
	if deliverysql.IsNilDB(db) {
		return nil
	}

	if typed, ok := db.(deliverysql.DeliveryDB); ok {
		return typed
	}

	return nil
}

func (r *DeliveryRepository) UpdateOutboxAggregateStatus(ctx context.Context, outboxID int64) error {
	if err := r.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}); err != nil {
		return fmt.Errorf("update outbox aggregate statuses: %w", err)
	}

	return nil
}

const outboxAggregateFailedErrorText = "per-room delivery failed"

// count-후-update 2단계는 reconcile vs aggregate-sync 경합에서 stale 집계로 되돌리는
// lost update가 있었다 — 집계와 갱신을 단문으로 원자화했으니 다시 쪼개지 말 것.
func (r *DeliveryRepository) UpdateOutboxAggregateStatuses(ctx context.Context, outboxIDs []int64) error {
	uniqueIDs := deliverysql.UniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	if _, err := r.db.Exec(ctx, mustSQL("delivery_repository_aggregate_sync.sql"),
		uniqueIDs,
		domain.OutboxStatusPending,
		DeliveryStatusSending,
		domain.OutboxStatusFailed,
		DeliveryStatusQuarantined,
		domain.OutboxStatusSent,
		dispatchstate.CanonicalSentAtNow(),
		outboxAggregateFailedErrorText,
	); err != nil {
		return fmt.Errorf("update outbox aggregate statuses: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) FindPendingOutboxIDsForAggregateSync(ctx context.Context, batchSize int) ([]int64, error) {
	if r == nil || r.db == nil || batchSize <= 0 {
		return nil, nil
	}

	var outboxIDs []int64

	if err := deliverysql.SelectDeliverySQL(ctx, r.db, &outboxIDs, "find pending outbox ids for aggregate sync", mustSQL("delivery_repository_0373_10.sql"), domain.OutboxStatusPending, domain.OutboxStatusPending, DeliveryStatusSending, batchSize); err != nil {
		return nil, fmt.Errorf("find pending outbox ids for aggregate sync: %w", err)
	}

	return outboxIDs, nil
}
