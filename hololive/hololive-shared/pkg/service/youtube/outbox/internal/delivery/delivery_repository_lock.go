package delivery

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type deliveryLockToken struct {
	id       int64
	lockedAt *time.Time
}

func (r *DeliveryRepository) MarkSentBatchIfLocked(ctx context.Context, tokens []deliveryLockToken, claimTokens ...deliveryClaimToken) error {
	uniqueTokens := uniqueDeliveryLockTokens(tokens)
	if len(uniqueTokens) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("mark delivery rows sent: db is nil")
	}

	sentAt := canonicalSentAtNow()
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updatedIDs, err := updateSentDeliveryRowsIfLocked(tx, uniqueTokens, sentAt)
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

func updateSentDeliveryRowsIfLocked(tx *gorm.DB, tokens []deliveryLockToken, sentAt time.Time) ([]int64, error) {
	updatedIDs := make([]int64, 0, len(tokens))
	for i := range tokens {
		if tokens[i].id == 0 || tokens[i].lockedAt == nil {
			continue
		}
		result := tx.Model(&domain.YouTubeNotificationDelivery{}).
			Where("id = ? AND status = ? AND locked_at = ?", tokens[i].id, domain.OutboxStatusPending, *tokens[i].lockedAt).
			Updates(map[string]any{
				"status":    domain.OutboxStatusSent,
				"sent_at":   sentAt,
				"locked_at": nil,
				"error":     "",
			})
		if result.Error != nil {
			return nil, fmt.Errorf("update delivery row %d: %w", tokens[i].id, result.Error)
		}
		if result.RowsAffected > 0 {
			updatedIDs = append(updatedIDs, tokens[i].id)
		}
	}
	return updatedIDs, nil
}

func (r *DeliveryRepository) MarkFailedRetryBatchIfLocked(ctx context.Context, tokens []deliveryLockToken, maxRetries int, backoff time.Duration, errMsg string) error {
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
		err := r.db.WithContext(ctx).Exec(`
			UPDATE youtube_notification_delivery
			SET attempt_count = attempt_count + 1,
			    error = ?,
			    status = CASE WHEN attempt_count + 1 >= ? THEN ? ELSE ? END,
			    next_attempt_at = CASE WHEN attempt_count + 1 >= ? THEN next_attempt_at ELSE ? END,
			    locked_at = NULL
			WHERE id = ? AND status = ? AND locked_at = ?
		`, truncateString(errMsg, 500), maxRetries, domain.OutboxStatusFailed, domain.OutboxStatusPending, maxRetries, nextAttempt, uniqueTokens[i].id, domain.OutboxStatusPending, *uniqueTokens[i].lockedAt).Error
		if err != nil {
			return fmt.Errorf("mark delivery row %d failed batch: %w", uniqueTokens[i].id, err)
		}
	}

	return nil
}

func (r *DeliveryRepository) MarkPermanentFailureBatchIfLocked(ctx context.Context, tokens []deliveryLockToken, maxRetries int, errMsg string) error {
	uniqueTokens := uniqueDeliveryLockTokens(tokens)
	if len(uniqueTokens) == 0 {
		return nil
	}

	for i := range uniqueTokens {
		if uniqueTokens[i].id == 0 || uniqueTokens[i].lockedAt == nil {
			continue
		}
		err := r.db.WithContext(ctx).Exec(`
			UPDATE youtube_notification_delivery
			SET attempt_count = CASE WHEN attempt_count >= ? THEN attempt_count ELSE ? END,
			    error = ?,
			    status = ?,
			    locked_at = NULL
			WHERE id = ? AND status = ? AND locked_at = ?
		`, maxRetries, maxRetries, truncateString(errMsg, 500), domain.OutboxStatusFailed, uniqueTokens[i].id, domain.OutboxStatusPending, *uniqueTokens[i].lockedAt).Error
		if err != nil {
			return fmt.Errorf("mark delivery row %d permanent failed batch: %w", uniqueTokens[i].id, err)
		}
	}

	return nil
}

func uniqueDeliveryLockTokens(tokens []deliveryLockToken) []deliveryLockToken {
	if len(tokens) == 0 {
		return nil
	}
	unique := make([]deliveryLockToken, 0, len(tokens))
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

func deliveryLockTokensForIDs(rows []domain.YouTubeNotificationDelivery, ids []int64) []deliveryLockToken {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return nil
	}
	lockedByID := make(map[int64]*time.Time, len(rows))
	for i := range rows {
		lockedByID[rows[i].ID] = rows[i].LockedAt
	}
	tokens := make([]deliveryLockToken, 0, len(uniqueIDs))
	for _, id := range uniqueIDs {
		tokens = append(tokens, deliveryLockToken{id: id, lockedAt: lockedByID[id]})
	}
	return tokens
}
