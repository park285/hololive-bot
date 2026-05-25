package delivery

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type StatusUpdater struct {
	db     *gorm.DB
	logger *slog.Logger
	config Config
}

type outboxLockToken struct {
	id       int64
	lockedAt *time.Time
}

func newStatusUpdater(db *gorm.DB, logger *slog.Logger, config Config) *StatusUpdater {
	if logger == nil {
		logger = slog.Default()
	}
	return &StatusUpdater{
		db:     db,
		logger: logger,
		config: config,
	}
}

func (u *StatusUpdater) markSent(ctx context.Context, id int64) {
	u.markSentBatch(ctx, []int64{id})
}

func (u *StatusUpdater) markSentIfLocked(ctx context.Context, id int64, lockedAt *time.Time) {
	if lockedAt == nil {
		u.markSent(ctx, id)
		return
	}
	u.markSentBatchIfLocked(ctx, []outboxLockToken{{id: id, lockedAt: lockedAt}})
}

const markSentBatchChunkSize = 500

func (u *StatusUpdater) markSentBatch(ctx context.Context, ids []int64) {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return
	}

	now := canonicalSentAtNow()
	for start := 0; start < len(uniqueIDs); start += markSentBatchChunkSize {
		end := min(start+markSentBatchChunkSize, len(uniqueIDs))
		chunk := uniqueIDs[start:end]

		result := u.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
			Where("id IN ? AND status = ?", chunk, domain.OutboxStatusPending).
			Updates(map[string]any{
				"status":    domain.OutboxStatusSent,
				"sent_at":   now,
				"locked_at": nil,
				"error":     "",
			})
		if result.Error != nil {
			u.logger.Error("Failed to mark outbox items as sent",
				slog.Int("batch_size", len(chunk)),
				slog.Any("error", result.Error))
		}
	}
}

func (u *StatusUpdater) markSentBatchIfLocked(ctx context.Context, tokens []outboxLockToken) {
	if len(tokens) == 0 {
		return
	}

	now := canonicalSentAtNow()
	for i := range tokens {
		if tokens[i].id == 0 || tokens[i].lockedAt == nil {
			continue
		}
		result := u.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
			Where("id = ? AND status = ? AND locked_at = ?", tokens[i].id, domain.OutboxStatusPending, *tokens[i].lockedAt).
			Updates(map[string]any{
				"status":    domain.OutboxStatusSent,
				"sent_at":   now,
				"locked_at": nil,
				"error":     "",
			})
		if result.Error != nil {
			u.logger.Error("Failed to mark outbox item as sent",
				slog.Int64("id", tokens[i].id),
				slog.Any("error", result.Error))
		}
	}
}

func (u *StatusUpdater) markFailed(ctx context.Context, id int64, errMsg string) {
	var item domain.YouTubeNotificationOutbox
	if err := u.db.WithContext(ctx).First(&item, id).Error; err != nil {
		u.logger.Warn("Failed to fetch outbox item for retry", slog.Int64("id", id), slog.Any("error", err))
		return
	}

	newAttemptCount := item.AttemptCount + 1
	if newAttemptCount >= u.config.MaxRetries {
		u.markFailedPermanently(ctx, id, newAttemptCount, errMsg)
		return
	}

	u.scheduleFailedRetry(ctx, id, newAttemptCount, errMsg)
}

func (u *StatusUpdater) markFailedIfLocked(ctx context.Context, id int64, lockedAt *time.Time, errMsg string) {
	if lockedAt == nil {
		u.markFailed(ctx, id, errMsg)
		return
	}

	var item domain.YouTubeNotificationOutbox
	if err := u.db.WithContext(ctx).
		Where("id = ? AND status = ? AND locked_at = ?", id, domain.OutboxStatusPending, *lockedAt).
		First(&item).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			u.logger.Warn("Failed to fetch locked outbox item for retry", slog.Int64("id", id), slog.Any("error", err))
		}
		return
	}

	newAttemptCount := item.AttemptCount + 1
	if newAttemptCount >= u.config.MaxRetries {
		u.markFailedPermanentlyIfLocked(ctx, outboxLockToken{id: id, lockedAt: lockedAt}, newAttemptCount, errMsg)
		return
	}

	u.scheduleFailedRetryIfLocked(ctx, outboxLockToken{id: id, lockedAt: lockedAt}, newAttemptCount, errMsg)
}

func (u *StatusUpdater) markFailedPermanently(ctx context.Context, id int64, attemptCount int, errMsg string) {
	result := u.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        domain.OutboxStatusFailed,
			"locked_at":     nil,
			"attempt_count": attemptCount,
			"error":         truncateString(errMsg, 500),
		})
	if result.Error != nil {
		u.logger.Error("Failed to mark outbox item as permanently failed",
			slog.Int64("id", id),
			slog.Any("error", result.Error))
	}
	u.logger.Warn("Outbox item permanently failed after max retries",
		slog.Int64("id", id),
		slog.Int("attempts", attemptCount))
}

func (u *StatusUpdater) markFailedPermanentlyIfLocked(ctx context.Context, token outboxLockToken, attemptCount int, errMsg string) {
	if token.lockedAt == nil {
		u.markFailedPermanently(ctx, token.id, attemptCount, errMsg)
		return
	}

	result := u.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ? AND status = ? AND locked_at = ?", token.id, domain.OutboxStatusPending, *token.lockedAt).
		Updates(map[string]any{
			"status":        domain.OutboxStatusFailed,
			"locked_at":     nil,
			"attempt_count": attemptCount,
			"error":         truncateString(errMsg, 500),
		})
	if result.Error != nil {
		u.logger.Error("Failed to mark outbox item as permanently failed",
			slog.Int64("id", token.id),
			slog.Any("error", result.Error))
	}
	if result.RowsAffected > 0 {
		u.logger.Warn("Outbox item permanently failed after max retries",
			slog.Int64("id", token.id),
			slog.Int("attempts", attemptCount))
	}
}

func (u *StatusUpdater) scheduleFailedRetry(ctx context.Context, id int64, attemptCount int, errMsg string) {
	nextAttempt := time.Now().Add(u.config.RetryBackoff * time.Duration(attemptCount))
	result := u.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"locked_at":       nil,
			"attempt_count":   attemptCount,
			"next_attempt_at": nextAttempt,
			"error":           truncateString(errMsg, 500),
		})
	if result.Error != nil {
		u.logger.Error("Failed to schedule outbox item for retry",
			slog.Int64("id", id),
			slog.Any("error", result.Error))
	}

	u.logger.Info("Outbox item scheduled for retry",
		slog.Int64("id", id),
		slog.Int("attempt", attemptCount),
		slog.Time("next_attempt", nextAttempt))
}

func (u *StatusUpdater) scheduleFailedRetryIfLocked(ctx context.Context, token outboxLockToken, attemptCount int, errMsg string) {
	if token.lockedAt == nil {
		u.scheduleFailedRetry(ctx, token.id, attemptCount, errMsg)
		return
	}

	nextAttempt := time.Now().Add(u.config.RetryBackoff * time.Duration(attemptCount))
	result := u.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ? AND status = ? AND locked_at = ?", token.id, domain.OutboxStatusPending, *token.lockedAt).
		Updates(map[string]any{
			"locked_at":       nil,
			"attempt_count":   attemptCount,
			"next_attempt_at": nextAttempt,
			"error":           truncateString(errMsg, 500),
		})
	if result.Error != nil {
		u.logger.Error("Failed to schedule outbox item for retry",
			slog.Int64("id", token.id),
			slog.Any("error", result.Error))
	}
	if result.RowsAffected > 0 {
		u.logger.Info("Outbox item scheduled for retry",
			slog.Int64("id", token.id),
			slog.Int("attempt", attemptCount),
			slog.Time("next_attempt", nextAttempt))
	}
}
