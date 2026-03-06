package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// markSent: 발송 완료 처리
func (d *Dispatcher) markSent(ctx context.Context, id int64) {
	d.markSentBatch(ctx, []int64{id})
}

const markSentBatchChunkSize = 500

func (d *Dispatcher) markSentBatch(ctx context.Context, ids []int64) {
	uniqueIDs := uniqueInt64s(ids)
	if len(uniqueIDs) == 0 {
		return
	}

	now := time.Now()
	for start := 0; start < len(uniqueIDs); start += markSentBatchChunkSize {
		end := start + markSentBatchChunkSize
		if end > len(uniqueIDs) {
			end = len(uniqueIDs)
		}
		chunk := uniqueIDs[start:end]

		result := d.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
			Where("id IN ? AND status = ?", chunk, domain.OutboxStatusPending).
			Updates(map[string]any{
				"status":    domain.OutboxStatusSent,
				"sent_at":   now,
				"locked_at": nil,
				"error":     "",
			})
		if result.Error != nil {
			d.logger.Error("Failed to mark outbox items as sent",
				slog.Int("batch_size", len(chunk)),
				slog.Any("error", result.Error))
		}
	}
}

// markFailed: 발송 실패 처리 (retry 지원)
func (d *Dispatcher) markFailed(ctx context.Context, id int64, errMsg string) {
	var item domain.YouTubeNotificationOutbox
	if err := d.db.WithContext(ctx).First(&item, id).Error; err != nil {
		d.logger.Warn("Failed to fetch outbox item for retry", slog.Int64("id", id), slog.Any("error", err))
		return
	}

	newAttemptCount := item.AttemptCount + 1
	if newAttemptCount >= d.cfg.MaxRetries {
		result := d.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"status":        domain.OutboxStatusFailed,
				"locked_at":     nil,
				"attempt_count": newAttemptCount,
				"error":         truncateString(errMsg, 500),
			})
		if result.Error != nil {
			d.logger.Error("Failed to mark outbox item as permanently failed",
				slog.Int64("id", id),
				slog.Any("error", result.Error))
		}
		d.logger.Warn("Outbox item permanently failed after max retries",
			slog.Int64("id", id),
			slog.Int("attempts", newAttemptCount))
		return
	}

	nextAttempt := time.Now().Add(d.cfg.RetryBackoff * time.Duration(newAttemptCount))
	result := d.db.WithContext(ctx).Model(&domain.YouTubeNotificationOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"locked_at":       nil,
			"attempt_count":   newAttemptCount,
			"next_attempt_at": nextAttempt,
			"error":           truncateString(errMsg, 500),
		})
	if result.Error != nil {
		d.logger.Error("Failed to schedule outbox item for retry",
			slog.Int64("id", id),
			slog.Any("error", result.Error))
	}

	d.logger.Info("Outbox item scheduled for retry",
		slog.Int64("id", id),
		slog.Int("attempt", newAttemptCount),
		slog.Time("next_attempt", nextAttempt))
}

func collectOutboxIDs(items []domain.YouTubeNotificationOutbox) []int64 {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	return ids
}

func uniqueInt64s(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

// truncateString: 문자열 길이 제한
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
