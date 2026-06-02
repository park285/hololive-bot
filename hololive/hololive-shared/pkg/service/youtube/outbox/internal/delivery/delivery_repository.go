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

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type DeliveryRepository struct {
	db     deliveryDB
	logger *slog.Logger
}

type deliveryStatusCount struct {
	OutboxID int64               `db:"outbox_id"`
	Status   domain.OutboxStatus `db:"status"`
	Count    int64               `db:"count"`
}

type deliveryAlarmSentTarget struct {
	Kind      domain.OutboxKind `db:"kind"`
	ContentID string            `db:"content_id"`
}

func NewDeliveryRepository(db any, logger *slog.Logger) *DeliveryRepository {
	return &DeliveryRepository{
		db:     asDeliveryDB(db),
		logger: logger,
	}
}

func asDeliveryDB(db any) deliveryDB {
	if isNilDB(db) {
		return nil
	}
	if typed, ok := db.(deliveryDB); ok {
		return typed
	}
	return nil
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

	valueExprs := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*5)
	for i := range rows {
		base := i*5 + 1
		valueExprs = append(valueExprs, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d)", base, base+1, base+2, base+3, base+4))
		args = append(args, rows[i].OutboxID, rows[i].RoomID, rows[i].Status, rows[i].AttemptCount, rows[i].NextAttemptAt)
	}

	if _, err := r.db.Exec(ctx, `
		INSERT INTO youtube_notification_delivery (outbox_id, room_id, status, attempt_count, next_attempt_at)
		VALUES `+strings.Join(valueExprs, ", ")+`
		ON CONFLICT (outbox_id, room_id) DO NOTHING
	`, args...); err != nil {
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

	pgxRows, err := r.db.Query(ctx, `
		WITH claim AS (
			SELECT id
			FROM youtube_notification_delivery
			WHERE status = $1
			  AND (locked_at IS NULL OR locked_at < $2)
			  AND next_attempt_at <= $3
			ORDER BY created_at ASC
			LIMIT $4
			FOR UPDATE SKIP LOCKED
		)
		UPDATE youtube_notification_delivery d
		SET locked_at = $5
		FROM claim
		WHERE d.id = claim.id
		RETURNING d.id, d.outbox_id, d.room_id, d.status, d.attempt_count,
		          d.next_attempt_at, d.created_at, d.locked_at, d.sent_at, COALESCE(d.error, '') AS error
	`, domain.OutboxStatusPending, lockExpiry, now, batchSize, now)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock delivery rows: %w", err)
	}
	defer pgxRows.Close()
	rows, err := pgx.CollectRows(pgxRows, scanDeliveryRow)
	if err != nil {
		return nil, fmt.Errorf("fetch and lock delivery rows: %w", err)
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
	if err := inDeliveryTx(ctx, r.db, func(tx dbx.Querier) error {
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
	claimTokens []deliveryClaimToken,
) error {
	trackingMarks, err := loadAlarmSentMarksForPendingDeliveryIDs(ctx, tx, uniqueIDs, sentAt, claimTokens)
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
	args = appendDeliveryInt64Args(args, uniqueIDs)
	args = append(args, domain.OutboxStatusPending)
	if _, err := execDeliverySQL(ctx, tx, "update delivery rows", `
		UPDATE youtube_notification_delivery
		SET status = ?, sent_at = ?, locked_at = NULL, error = ''
		WHERE `+deliveryInClause("id", len(uniqueIDs))+` AND status = ?
	`, args...); err != nil {
		return err
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

	_, err := r.db.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET attempt_count = attempt_count + 1,
		    error = $1,
		    status = CASE WHEN attempt_count + 1 >= $2 THEN $3 ELSE $4 END,
		    next_attempt_at = CASE WHEN attempt_count + 1 >= $5 THEN next_attempt_at ELSE $6 END,
		    locked_at = NULL
		WHERE id = $7
	`, truncateString(errMsg, 500), maxRetries, domain.OutboxStatusFailed, domain.OutboxStatusPending, maxRetries, nextAttempt, id)
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

	args := []any{truncateString(errMsg, 500), maxRetries, domain.OutboxStatusFailed, domain.OutboxStatusPending, maxRetries, nextAttempt}
	args = appendDeliveryInt64Args(args, uniqueIDs)
	if _, err := execDeliverySQL(ctx, r.db, "mark delivery rows failed batch", `
		UPDATE youtube_notification_delivery
		SET attempt_count = attempt_count + 1,
		    error = ?,
		    status = CASE WHEN attempt_count + 1 >= ? THEN ? ELSE ? END,
		    next_attempt_at = CASE WHEN attempt_count + 1 >= ? THEN next_attempt_at ELSE ? END,
		    locked_at = NULL
		WHERE `+deliveryInClause("id", len(uniqueIDs))+`
	`, args...); err != nil {
		return err
	}

	return nil
}

func (r *DeliveryRepository) MarkPermanentFailureBatch(ctx context.Context, ids []int64, maxRetries int, errMsg string) error {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}

	args := []any{maxRetries, maxRetries, truncateString(errMsg, 500), domain.OutboxStatusFailed}
	args = appendDeliveryInt64Args(args, uniqueIDs)
	args = append(args, domain.OutboxStatusPending)
	if _, err := execDeliverySQL(ctx, r.db, "mark delivery rows permanent failed batch", `
		UPDATE youtube_notification_delivery
		SET attempt_count = CASE WHEN attempt_count >= ? THEN attempt_count ELSE ? END,
		    error = ?,
		    status = ?,
		    locked_at = NULL
		WHERE `+deliveryInClause("id", len(uniqueIDs))+` AND status = ?
	`, args...); err != nil {
		return err
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
	if err := selectDeliverySQL(ctx, r.db, &counts, "count delivery statuses", `
		SELECT outbox_id, status, COUNT(*) AS count
		FROM youtube_notification_delivery
		WHERE `+deliveryInClause("outbox_id", len(uniqueIDs))+`
		GROUP BY outbox_id, status
	`, appendDeliveryInt64Args(nil, uniqueIDs)...); err != nil {
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

	sentAt := canonicalSentAtNow()
	errorText := ""
	switch status {
	case domain.OutboxStatusSent:
	case domain.OutboxStatusFailed:
		errorText = "per-room delivery failed"
	}

	args := []any{
		status,
		status, domain.OutboxStatusSent, sentAt,
		status, domain.OutboxStatusFailed, errorText,
	}
	args = appendDeliveryInt64Args(args, uniqueIDs)
	if _, err := execDeliverySQL(ctx, r.db, "update outbox aggregate statuses: apply update", `
		UPDATE youtube_notification_outbox
		SET status = ?::text,
		    locked_at = NULL,
		    sent_at = CASE WHEN ?::text = ?::text THEN ? ELSE sent_at END,
		    error = CASE WHEN ?::text = ?::text THEN ? ELSE '' END
		WHERE `+deliveryInClause("id", len(uniqueIDs))+`
	`, args...); err != nil {
		return err
	}

	return nil
}

func (r *DeliveryRepository) FindPendingOutboxIDsForAggregateSync(ctx context.Context, batchSize int) ([]int64, error) {
	if r == nil || r.db == nil || batchSize <= 0 {
		return nil, nil
	}

	var outboxIDs []int64
	if err := selectDeliverySQL(ctx, r.db, &outboxIDs, "find pending outbox ids for aggregate sync", `
		SELECT d.outbox_id
		FROM youtube_notification_delivery d
		JOIN youtube_notification_outbox o ON o.id = d.outbox_id
		WHERE o.status = ?
		GROUP BY d.outbox_id
		HAVING SUM(CASE WHEN d.status = ? THEN 1 ELSE 0 END) = 0
		ORDER BY d.outbox_id ASC
		LIMIT ?
	`, domain.OutboxStatusPending, domain.OutboxStatusPending, batchSize); err != nil {
		return nil, fmt.Errorf("find pending outbox ids for aggregate sync: %w", err)
	}

	return outboxIDs, nil
}
