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

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
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

// UpdateOutboxAggregateStatuses는 부모 outbox 집계를 자체 READ COMMITTED 트랜잭션 하나로 다시 계산한다. 부모를 ID
// 순서로 잠근 뒤 다음 statement에서 자식을 집계한다.
func (r *DeliveryRepository) UpdateOutboxAggregateStatuses(ctx context.Context, outboxIDs []int64) error {
	uniqueIDs := deliverysql.UniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	if err := pgx.BeginTxFunc(ctx, r.db, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if err := lockOutboxAggregateParents(ctx, tx, uniqueIDs); err != nil {
			return err
		}

		return syncOutboxAggregates(ctx, tx, uniqueIDs)
	}); err != nil {
		return fmt.Errorf("project outbox aggregate transaction: %w", err)
	}

	return nil
}

// lockOutboxAggregateParents는 호출자의 READ COMMITTED 트랜잭션에서 중복 없는 부모 outbox를 ID 순서로 FOR NO KEY
// UPDATE 잠근다. 이미 지워진 부모는 잠금 결과에 없다. 집계는 같은 트랜잭션의 뒤 statement(syncOutboxAggregates)가
// 하므로, 자식을 함께 바꾸는 호출자는 이 잠금을 자식 잠금보다 먼저 건다(보존 정리·fanout과 같은 부모 → 자식 순서).
func lockOutboxAggregateParents(ctx context.Context, tx dbx.Querier, outboxIDs []int64) error {
	if len(outboxIDs) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, mustSQL("delivery_repository_aggregate_lock.sql"), outboxIDs); err != nil {
		return fmt.Errorf("lock outbox aggregate parents: %w", err)
	}

	return nil
}

// syncOutboxAggregates는 lockOutboxAggregateParents로 이미 잠근 중복 없는 부모의 집계를 canonical 집계 문장으로 다시
// 계산하며 부모를 다시 잠그지 않는다. 부모 잠금 뒤의 statement라 잠금 대기 전 snapshot으로 더 늦게 확정된 SENT를
// 덮어쓰지 않는다. 같은 트랜잭션이 앞서 바꾼 자식도 집계에 들어가므로 자식 전이와 함께 쓰면 부모 집계가 그 전이와 함께
// commit되거나 함께 되돌아간다. 새로 기록하는 outbox sent_at·terminal_at은 집계 문장이 채취한 DB 시계다.
func syncOutboxAggregates(ctx context.Context, tx dbx.Querier, outboxIDs []int64) error {
	if len(outboxIDs) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, mustSQL("delivery_repository_aggregate_sync.sql"),
		outboxIDs,
		domain.OutboxStatusPending,
		DeliveryStatusSending,
		domain.OutboxStatusFailed,
		DeliveryStatusQuarantined,
		domain.OutboxStatusSent,
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

	if err := dbx.SelectSQL(ctx, r.db, &outboxIDs, "find pending outbox ids for aggregate sync", mustSQL("delivery_repository_0373_10.sql"), batchSize); err != nil {
		return nil, fmt.Errorf("find pending outbox ids for aggregate sync: %w", err)
	}

	return outboxIDs, nil
}
