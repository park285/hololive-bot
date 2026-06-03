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

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
)

type LockToken struct {
	id       int64
	lockedAt *time.Time
}

func NewLockToken(id int64, lockedAt *time.Time) LockToken {
	return LockToken{id: id, lockedAt: lockedAt}
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
		trackingMarks, err := loadAlarmSentMarksForDeliveryIDs(ctx, tx, updatedIDs, sentAt, claimTokens)
		if err != nil {
			return fmt.Errorf("load tracking marks: %w", err)
		}
		return persistSentDeliveryTracking(ctx, tx, trackingMarks)
	}); err != nil {
		return fmt.Errorf("mark delivery rows sent transaction: %w", err)
	}

	return nil
}

func updateSentDeliveryRowsIfLocked(ctx context.Context, tx dbx.Querier, tokens []LockToken, sentAt time.Time) ([]int64, error) {
	updatedIDs := make([]int64, 0, len(tokens))
	for i := range tokens {
		if tokens[i].id == 0 || tokens[i].lockedAt == nil {
			continue
		}
		tag, err := tx.Exec(ctx, `
			UPDATE youtube_notification_delivery
			SET status = $1, sent_at = $2, locked_at = NULL, error = ''
			WHERE id = $3 AND status = $4
			  AND locked_at = $5
		`, domain.OutboxStatusSent, sentAt, tokens[i].id, domain.OutboxStatusPending, *tokens[i].lockedAt)
		if err != nil {
			return nil, fmt.Errorf("update delivery row %d: %w", tokens[i].id, err)
		}
		if tag.RowsAffected() > 0 {
			updatedIDs = append(updatedIDs, tokens[i].id)
		}
	}
	return updatedIDs, nil
}

func (r *DeliveryRepository) MarkFailedRetryBatchIfLocked(ctx context.Context, tokens []LockToken, maxRetries int, backoff time.Duration, errMsg string) error {
	uniqueTokens := uniqueDeliveryLockTokens(tokens)
	if len(uniqueTokens) == 0 {
		return nil
	}

	now := time.Now()
	nextAttempt := now.Add(backoff)

	for i := range uniqueTokens {
		if uniqueTokens[i].id == 0 || uniqueTokens[i].lockedAt == nil {
			continue
		}
		_, err := r.db.Exec(ctx, `
			UPDATE youtube_notification_delivery
			SET attempt_count = attempt_count + 1,
			    error = $1,
			    status = CASE WHEN attempt_count + 1 >= $2 THEN $3 ELSE $4 END,
			    next_attempt_at = CASE WHEN attempt_count + 1 >= $5 THEN next_attempt_at ELSE $6 END,
			    locked_at = NULL
			WHERE id = $7 AND status = $8 AND locked_at = $9
		`, deliverysql.TruncateString(errMsg, 500), maxRetries, domain.OutboxStatusFailed, domain.OutboxStatusPending, maxRetries, nextAttempt, uniqueTokens[i].id, domain.OutboxStatusPending, *uniqueTokens[i].lockedAt)
		if err != nil {
			return fmt.Errorf("mark delivery row %d failed batch: %w", uniqueTokens[i].id, err)
		}
	}

	return nil
}

func (r *DeliveryRepository) MarkPermanentFailureBatchIfLocked(ctx context.Context, tokens []LockToken, maxRetries int, errMsg string) error {
	uniqueTokens := uniqueDeliveryLockTokens(tokens)
	if len(uniqueTokens) == 0 {
		return nil
	}

	for i := range uniqueTokens {
		if uniqueTokens[i].id == 0 || uniqueTokens[i].lockedAt == nil {
			continue
		}
		_, err := r.db.Exec(ctx, `
			UPDATE youtube_notification_delivery
			SET attempt_count = CASE WHEN attempt_count >= $1 THEN attempt_count ELSE $2 END,
			    error = $3,
			    status = $4,
			    locked_at = NULL
			WHERE id = $5 AND status = $6 AND locked_at = $7
		`, maxRetries, maxRetries, deliverysql.TruncateString(errMsg, 500), domain.OutboxStatusFailed, uniqueTokens[i].id, domain.OutboxStatusPending, *uniqueTokens[i].lockedAt)
		if err != nil {
			return fmt.Errorf("mark delivery row %d permanent failed batch: %w", uniqueTokens[i].id, err)
		}
	}

	return nil
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
