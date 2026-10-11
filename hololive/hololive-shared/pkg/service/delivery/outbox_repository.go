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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

type outboxBatchRow struct {
	Kind      domain.DeliveryOutboxKind `json:"kind"`
	PeriodKey string                    `json:"period_key"`
	RoomID    string                    `json:"room_id"`
	ContentID string                    `json:"content_id"`
	Payload   deliverypayload.Payload   `json:"payload"`
}

// OutboxRepository는 notification_delivery_outbox의 producer 적재를 소유한다.
// Claim·발송 정산·격리·보존 정리는 alarm-worker notificationdelivery.Store가 맡는다.
// 단일 문장 쓰기와 행 잠금·status·locked_by·attempt_count 조건으로 동시성을 제어한다.
type OutboxRepository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

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
	if err := r.EnqueueBatch(ctx, []OutboxItem{
		{
			Kind:      kind,
			PeriodKey: periodKey,
			RoomID:    roomID,
			Message:   message,
		},
	}); err != nil {
		return fmt.Errorf("enqueue batch: %w", err)
	}

	return nil
}

// EnqueueBatch는 (kind, content_id) 충돌 시 FAILED 행만 PENDING으로 rearm한다. PENDING·SENDING·SENT·QUARANTINED 행은
// 그대로 두며, worker가 저장한 request snapshot이 있으면 room_id와 payload를 덮어쓰지 않는다.
func (r *OutboxRepository) EnqueueBatch(ctx context.Context, items []OutboxItem) error {
	if len(items) == 0 {
		return nil
	}

	if err := r.ensurePool(); err != nil {
		return fmt.Errorf("ensure pool: %w", err)
	}

	rows := make([]outboxBatchRow, 0, len(items))
	for _, item := range items {
		contentID := item.PeriodKey + ":" + item.RoomID

		rows = append(rows, outboxBatchRow{
			Kind:      item.Kind,
			PeriodKey: item.PeriodKey,
			RoomID:    item.RoomID,
			ContentID: contentID,
			Payload:   deliverypayload.Payload{Message: item.Message},
		})
	}

	raw, err := jsonv2.Marshal(rows)
	if err != nil {
		return fmt.Errorf("enqueue batch: marshal rows: %w", err)
	}

	if _, err := r.pool.Exec(ctx, mustSQL("outbox_enqueue_batch_upsert.sql"), string(raw)); err != nil {
		return fmt.Errorf("enqueue batch: %w", err)
	}

	return nil
}

func (r *OutboxRepository) ensurePool() error {
	if r == nil || r.pool == nil {
		return errors.New("notification delivery outbox repository: postgres pool is required")
	}

	return nil
}
