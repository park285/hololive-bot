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
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/telemetry"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type DeliveryRepository struct {
	db     deliverysql.DeliveryDB
	logger *slog.Logger
}

type deliveryAlarmSentTarget struct {
	Kind      domain.OutboxKind `db:"kind"`
	ContentID string            `db:"content_id"`
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

func (r *DeliveryRepository) EnqueueBatch(ctx context.Context, outboxID int64, roomIDs []string) error {
	uniqueRoomIDs := UniqueStrings(roomIDs)
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

	valueExprs := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*5)
	for i := range rows {
		base := i*5 + 1
		valueExprs = append(valueExprs, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d)", base, base+1, base+2, base+3, base+4))
		args = append(args, rows[i].OutboxID, rows[i].RoomID, rows[i].Status, rows[i].AttemptCount, rows[i].NextAttemptAt)
	}

	if _, err := r.db.Exec(ctx, mustSQL("delivery_repository_0100_01.sql")+strings.Join(valueExprs, ", ")+mustSQL("delivery_repository_0102_02.sql"), args...); err != nil {
		return fmt.Errorf("enqueue delivery batch: create rows: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) FetchAndLock(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.YouTubeNotificationDelivery, error) {
	if r == nil || r.db == nil || batchSize <= 0 {
		return nil, nil
	}

	lockExpiry := time.Now().Add(-lockTimeout)
	now := time.Now()

	pgxRows, err := r.db.Query(ctx, mustSQL("delivery_repository_0119_03.sql"), domain.OutboxStatusPending, lockExpiry, now, batchSize, now)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock delivery rows: %w", err)
	}
	defer pgxRows.Close()
	rows, err := pgx.CollectRows(pgxRows, deliverysql.ScanDeliveryRow)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock delivery rows: %w", err)
	}
	return rows, nil
}

func (r *DeliveryRepository) MarkSentBatch(ctx context.Context, ids []int64, claimTokens ...dispatchstate.ClaimToken) error {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("mark delivery rows sent: db is nil")
	}

	sentAt := dispatchstate.CanonicalSentAtNow()
	if err := deliverysql.InDeliveryTx(ctx, r.db, func(tx dbx.Querier) error {
		return markSentBatchTx(ctx, tx, uniqueIDs, sentAt, claimTokens)
	}); err != nil {
		return fmt.Errorf("mark delivery rows sent transaction: %w", err)
	}

	return nil
}

func markSentBatchTx(
	ctx context.Context,
	tx dbx.Querier,
	uniqueIDs []int64,
	sentAt time.Time,
	claimTokens []dispatchstate.ClaimToken,
) error {
	trackingMarks, err := LoadAlarmSentMarksForPendingDeliveryIDs(ctx, tx, uniqueIDs, sentAt, claimTokens)
	if err != nil {
		return fmt.Errorf("load tracking marks: %w", err)
	}
	if err := updateSentDeliveryRows(ctx, tx, uniqueIDs, sentAt); err != nil {
		return err
	}
	return persistSentDeliveryTracking(ctx, tx, trackingMarks)
}

func updateSentDeliveryRows(ctx context.Context, tx dbx.Querier, uniqueIDs []int64, sentAt time.Time) error {
	args := []any{domain.OutboxStatusSent, sentAt}
	args = deliverysql.AppendDeliveryInt64Args(args, uniqueIDs)
	args = append(args, domain.OutboxStatusPending)
	if _, err := deliverysql.ExecDeliverySQL(ctx, tx, "update delivery rows", mustSQL("delivery_repository_0188_04.sql")+deliverysql.DeliveryInClause("id", len(uniqueIDs))+` AND status = ?
	`, args...); err != nil {
		return fmt.Errorf("update delivery rows: %w", err)
	}
	return nil
}

func persistSentDeliveryTracking(
	ctx context.Context,
	tx dbx.Querier,
	trackingMarks []trackingrepo.AlarmSentMark,
) error {
	if err := trackingrepo.NewRepository(tx).MarkAlarmSentBatch(ctx, trackingMarks); err != nil {
		return fmt.Errorf("update tracking rows: %w", err)
	}
	return persistSentDeliveryLatencyClassifications(ctx, tx, trackingMarks)
}

func persistSentDeliveryLatencyClassifications(
	ctx context.Context,
	tx dbx.Querier,
	trackingMarks []trackingrepo.AlarmSentMark,
) error {
	if len(trackingMarks) == 0 {
		return nil
	}
	identities := make([]timeline.PostTrackingIdentity, 0, len(trackingMarks))
	for i := range trackingMarks {
		identities = append(identities, timeline.PostTrackingIdentity{Kind: trackingMarks[i].Kind, ContentID: trackingMarks[i].ContentID})
	}
	if err := telemetry.NewRepository(tx).PersistPostLatencyClassificationsByIdentities(ctx, identities); err != nil {
		return fmt.Errorf("persist tracking latency classifications: %w", err)
	}
	return nil
}

func (r *DeliveryRepository) MarkFailed(ctx context.Context, id int64, maxRetries int, backoff time.Duration, errMsg string) error {
	now := time.Now()
	nextAttempt := now.Add(backoff)

	_, err := r.db.Exec(ctx, mustSQL("delivery_repository_0231_05.sql"), deliverysql.TruncateString(errMsg, 500), maxRetries, domain.OutboxStatusFailed, domain.OutboxStatusPending, maxRetries, nextAttempt, id)
	if err != nil {
		return fmt.Errorf("mark delivery row failed: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) MarkFailedRetryBatch(ctx context.Context, ids []int64, maxRetries int, backoff time.Duration, errMsg string) error {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	now := time.Now()
	nextAttempt := now.Add(backoff)

	args := []any{deliverysql.TruncateString(errMsg, 500), maxRetries, domain.OutboxStatusFailed, domain.OutboxStatusPending, maxRetries, nextAttempt}
	args = deliverysql.AppendDeliveryInt64Args(args, uniqueIDs)
	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "mark delivery rows failed batch", mustSQL("delivery_repository_0258_06.sql")+deliverysql.DeliveryInClause("id", len(uniqueIDs))+`
	`, args...); err != nil {
		return fmt.Errorf("mark delivery rows failed batch: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) MarkPermanentFailureBatch(ctx context.Context, ids []int64, maxRetries int, errMsg string) error {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	args := []any{maxRetries, maxRetries, deliverysql.TruncateString(errMsg, 500), domain.OutboxStatusFailed}
	args = deliverysql.AppendDeliveryInt64Args(args, uniqueIDs)
	args = append(args, domain.OutboxStatusPending)
	if _, err := deliverysql.ExecDeliverySQL(ctx, r.db, "mark delivery rows permanent failed batch", mustSQL("delivery_repository_0282_07.sql")+deliverysql.DeliveryInClause("id", len(uniqueIDs))+` AND status = ?
	`, args...); err != nil {
		return fmt.Errorf("mark delivery rows permanent failed batch: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) UpdateOutboxAggregateStatus(ctx context.Context, outboxID int64) error {
	return r.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID})
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
