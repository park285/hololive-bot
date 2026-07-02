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
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
)

const (
	DeliveryStatusSending     domain.OutboxStatus = "SENDING"
	DeliveryStatusQuarantined domain.OutboxStatus = "QUARANTINED"
)

type LockToken struct {
	id       int64
	lockedAt *time.Time
}

func NewLockToken(id int64, lockedAt *time.Time) LockToken {
	return LockToken{id: id, lockedAt: lockedAt}
}

func (r *DeliveryRepository) MarkSendingBatchIfLocked(ctx context.Context, tokens []LockToken) ([]domain.YouTubeNotificationDelivery, error) {
	uniqueTokens := uniqueDeliveryLockTokens(tokens)
	if len(uniqueTokens) == 0 {
		return nil, nil
	}
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("mark delivery rows sending: db is nil")
	}

	ids, lockedAts := deliveryLockTokenArrays(uniqueTokens)
	if len(ids) == 0 {
		return nil, nil
	}

	sendingStartedAt := time.Now().UTC()
	rows, err := r.db.Query(ctx, `
		WITH input AS (
			SELECT *
			FROM unnest($1::bigint[], $2::timestamptz[]) WITH ORDINALITY AS t(id, locked_at, ord)
		), updated AS (
			UPDATE youtube_notification_delivery d
			SET status = $3, locked_at = $4
			FROM input i
			WHERE d.id = i.id
			  AND d.status = $5
			  AND d.locked_at = i.locked_at
			RETURNING i.ord, d.id, d.outbox_id, d.room_id, d.status, d.attempt_count,
			          d.next_attempt_at, d.created_at, d.locked_at, d.sent_at, COALESCE(d.error, '') AS error
		)
		SELECT id, outbox_id, room_id, status, attempt_count,
		       next_attempt_at, created_at, locked_at, sent_at, error
		FROM updated
		ORDER BY ord ASC
	`, ids, lockedAts, DeliveryStatusSending, sendingStartedAt, domain.OutboxStatusPending)
	if err != nil {
		return nil, fmt.Errorf("mark delivery rows sending: %w", err)
	}
	defer rows.Close()

	updated, err := pgx.CollectRows(rows, deliverysql.ScanDeliveryRow)
	if err != nil {
		return nil, fmt.Errorf("mark delivery rows sending: %w", err)
	}
	return updated, nil
}

func (r *DeliveryRepository) MarkSentBatchIfLocked(ctx context.Context, tokens []LockToken, claimTokens ...dispatchstate.ClaimToken) error {
	uniqueTokens := uniqueDeliveryLockTokens(tokens)
	if len(uniqueTokens) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("mark delivery rows sent: db is nil")
	}

	sentAt := dispatchstate.CanonicalSentAtNow()
	if err := deliverysql.InDeliveryTx(ctx, r.db, func(tx dbx.Querier) error {
		updatedIDs, err := updateSentDeliveryRowsIfLocked(ctx, tx, uniqueTokens, sentAt)
		if err != nil {
			return err
		}
		trackingMarks, err := LoadAlarmSentMarksForDeliveryIDs(ctx, tx, updatedIDs, sentAt, claimTokens)
		if err != nil {
			return fmt.Errorf("load tracking marks: %w", err)
		}
		return persistSentDeliveryTracking(ctx, tx, trackingMarks)
	}); err != nil {
		return fmt.Errorf("mark delivery rows sent transaction: %w", err)
	}

	return nil
}

func deliveryLockTokenArrays(tokens []LockToken) ([]int64, []time.Time) {
	ids := make([]int64, 0, len(tokens))
	lockedAts := make([]time.Time, 0, len(tokens))
	for i := range tokens {
		if tokens[i].id == 0 || tokens[i].lockedAt == nil {
			continue
		}
		ids = append(ids, tokens[i].id)
		lockedAts = append(lockedAts, *tokens[i].lockedAt)
	}
	return ids, lockedAts
}

func deliveryLockTokenIDs(tokens []LockToken) []int64 {
	ids, _ := deliveryLockTokenArrays(tokens)
	return ids
}

func updateSentDeliveryRowsIfLocked(ctx context.Context, tx dbx.Querier, tokens []LockToken, sentAt time.Time) ([]int64, error) {
	ids, lockedAts := deliveryLockTokenArrays(tokens)
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := tx.Query(ctx, `
		WITH input AS (
			SELECT *
			FROM unnest($1::bigint[], $2::timestamptz[]) AS t(id, locked_at)
		), updated AS (
			UPDATE youtube_notification_delivery d
			SET status = $3, sent_at = $4, locked_at = NULL, error = ''
			FROM input i
			WHERE d.id = i.id
			  AND d.status = $5
			  AND d.locked_at = i.locked_at
			RETURNING d.id
		)
		SELECT id FROM updated
	`, ids, lockedAts, domain.OutboxStatusSent, sentAt, DeliveryStatusSending)
	if err != nil {
		return nil, fmt.Errorf("batch update sent delivery rows: %w", err)
	}
	defer rows.Close()

	updatedIDs := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan updated delivery id: %w", err)
		}
		updatedIDs = append(updatedIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate updated delivery ids: %w", err)
	}
	return updatedIDs, nil
}

func (r *DeliveryRepository) MarkFailedRetryBatchIfLocked(ctx context.Context, tokens []LockToken, maxRetries int, backoff time.Duration, errMsg string) error {
	uniqueTokens := uniqueDeliveryLockTokens(tokens)
	if len(uniqueTokens) == 0 {
		return nil
	}

	ids, lockedAts := deliveryLockTokenArrays(uniqueTokens)
	if len(ids) == 0 {
		return nil
	}

	now := time.Now()
	nextAttempt := now.Add(backoff)

	_, err := r.db.Exec(ctx, `
		WITH input AS (
			SELECT *
			FROM unnest($1::bigint[], $2::timestamptz[]) AS t(id, locked_at)
		)
		UPDATE youtube_notification_delivery d
		SET attempt_count = attempt_count + 1,
		    error = $3,
		    status = CASE WHEN attempt_count + 1 >= $4 THEN $5 ELSE $6 END,
		    next_attempt_at = CASE WHEN attempt_count + 1 >= $4 THEN next_attempt_at ELSE $7 END,
		    locked_at = NULL
		FROM input i
		WHERE d.id = i.id
		  AND d.status = $8
		  AND d.locked_at = i.locked_at
	`, ids, lockedAts, deliverysql.TruncateString(errMsg, 500), maxRetries, domain.OutboxStatusFailed, domain.OutboxStatusPending, nextAttempt, DeliveryStatusSending)
	if err != nil {
		return fmt.Errorf("batch mark failed retry delivery rows: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) MarkPermanentFailureBatchIfLocked(ctx context.Context, tokens []LockToken, maxRetries int, errMsg string) error {
	uniqueTokens := uniqueDeliveryLockTokens(tokens)
	if len(uniqueTokens) == 0 {
		return nil
	}

	ids, lockedAts := deliveryLockTokenArrays(uniqueTokens)
	if len(ids) == 0 {
		return nil
	}

	_, err := r.db.Exec(ctx, `
		WITH input AS (
			SELECT *
			FROM unnest($1::bigint[], $2::timestamptz[]) AS t(id, locked_at)
		)
		UPDATE youtube_notification_delivery d
		SET attempt_count = CASE WHEN attempt_count >= $3 THEN attempt_count ELSE $3 END,
		    error = $4,
		    status = $5,
		    locked_at = NULL
		FROM input i
		WHERE d.id = i.id
		  AND d.status = $6
		  AND d.locked_at = i.locked_at
	`, ids, lockedAts, maxRetries, deliverysql.TruncateString(errMsg, 500), domain.OutboxStatusFailed, DeliveryStatusSending)
	if err != nil {
		return fmt.Errorf("batch mark permanent failure delivery rows: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) ([]int64, int, error) {
	if r == nil || r.db == nil || limit <= 0 {
		return nil, 0, nil
	}
	if olderThan <= 0 {
		olderThan = 5 * time.Minute
	}

	cutoff := time.Now().UTC().Add(-olderThan)
	rows, err := r.db.Query(ctx, `
		WITH picked AS (
			SELECT id
			FROM youtube_notification_delivery
			WHERE status = $1
			  AND locked_at IS NOT NULL
			  AND locked_at < $2
			ORDER BY locked_at ASC, id ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		), updated AS (
			UPDATE youtube_notification_delivery d
			SET status = $4,
			    attempt_count = attempt_count + 1,
			    locked_at = NULL,
			    error = $5
			FROM picked
			WHERE d.id = picked.id
			RETURNING d.outbox_id
		)
		SELECT outbox_id FROM updated
	`, DeliveryStatusSending, cutoff, limit, DeliveryStatusQuarantined, "stale sending; external send outcome unknown")
	if err != nil {
		return nil, 0, fmt.Errorf("quarantine stale sending delivery rows: %w", err)
	}
	defer rows.Close()

	outboxIDs := make([]int64, 0, limit)
	for rows.Next() {
		var outboxID int64
		if err := rows.Scan(&outboxID); err != nil {
			return nil, 0, fmt.Errorf("scan quarantined delivery outbox id: %w", err)
		}
		outboxIDs = append(outboxIDs, outboxID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate quarantined delivery outbox ids: %w", err)
	}

	return deliverysql.UniqueInt64s(outboxIDs), len(outboxIDs), nil
}

func uniqueDeliveryLockTokens(tokens []LockToken) []LockToken {
	if len(tokens) == 0 {
		return nil
	}
	unique := make([]LockToken, 0, len(tokens))
	seen := make(map[int64]struct{}, len(tokens))
	for i := range tokens {
		if tokens[i].id == 0 {
			continue
		}
		if _, ok := seen[tokens[i].id]; ok {
			continue
		}
		seen[tokens[i].id] = struct{}{}
		unique = append(unique, tokens[i])
	}
	return unique
}

func DeliveryLockTokensForIDs(rows []domain.YouTubeNotificationDelivery, ids []int64) []LockToken {
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}
	lockedByID := make(map[int64]*time.Time, len(rows))
	for i := range rows {
		lockedByID[rows[i].ID] = rows[i].LockedAt
	}
	tokens := make([]LockToken, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		tokens = append(tokens, LockToken{id: id, lockedAt: lockedByID[id]})
	}
	return tokens
}
