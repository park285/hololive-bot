package dispatch

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
)

type StatusUpdater struct {
	db     dbx.Querier
	logger *slog.Logger
	config Config
}

type outboxLockToken struct {
	id       int64
	lockedAt *time.Time
}

func newStatusUpdater(db any, logger *slog.Logger, config Config) *StatusUpdater {
	if logger == nil {
		logger = slog.Default()
	}
	return &StatusUpdater{
		db:     deliverysql.AsQuerier(db),
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
	uniqueIDs := deliverysql.UniqueInt64s(ids)
	if u == nil || u.db == nil || len(uniqueIDs) == 0 {
		return
	}

	now := dispatchstate.CanonicalSentAtNow()
	for start := 0; start < len(uniqueIDs); start += markSentBatchChunkSize {
		end := min(start+markSentBatchChunkSize, len(uniqueIDs))
		chunk := uniqueIDs[start:end]

		args := []any{domain.OutboxStatusSent, now}
		args = deliverysql.AppendDeliveryInt64Args(args, chunk)
		args = append(args, domain.OutboxStatusPending)
		_, err := deliverysql.ExecDeliverySQL(ctx, u.db, "mark outbox items sent", `
			UPDATE youtube_notification_outbox
			SET status = ?, sent_at = ?, locked_at = NULL, error = ''
			WHERE `+deliverysql.DeliveryInClause("id", len(chunk))+` AND status = ?
		`, args...)
		if err != nil {
			u.logger.Error("Failed to mark outbox items as sent",
				slog.Int("batch_size", len(chunk)),
				slog.Any("error", err))
		}
	}
}

type batchQuerier interface {
	SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults
}

func (u *StatusUpdater) markSentBatchIfLocked(ctx context.Context, tokens []outboxLockToken) {
	if u == nil || u.db == nil || len(tokens) == 0 {
		return
	}

	live := filterLiveLockTokens(tokens)
	if len(live) == 0 {
		return
	}

	now := dispatchstate.CanonicalSentAtNow()
	if batcher, ok := u.db.(batchQuerier); ok {
		u.markSentTokensBatch(ctx, batcher, live, now)
		return
	}
	u.markSentTokensSequential(ctx, live, now)
}

func filterLiveLockTokens(tokens []outboxLockToken) []outboxLockToken {
	live := make([]outboxLockToken, 0, len(tokens))
	for i := range tokens {
		if tokens[i].id == 0 || tokens[i].lockedAt == nil {
			continue
		}
		live = append(live, tokens[i])
	}
	return live
}

func (u *StatusUpdater) markSentTokensSequential(ctx context.Context, tokens []outboxLockToken, now time.Time) {
	for i := range tokens {
		u.markSentTokenIfLocked(ctx, tokens[i], now)
	}
}

const markSentIfLockedSQL = `
		UPDATE youtube_notification_outbox
		SET status = $1, sent_at = $2, locked_at = NULL, error = ''
		WHERE id = $3 AND status = $4 AND locked_at = $5
	`

func (u *StatusUpdater) markSentTokensBatch(ctx context.Context, batcher batchQuerier, tokens []outboxLockToken, now time.Time) {
	batch := &pgx.Batch{}
	for i := range tokens {
		batch.Queue(markSentIfLockedSQL,
			domain.OutboxStatusSent, now, tokens[i].id, domain.OutboxStatusPending, *tokens[i].lockedAt)
	}
	results := batcher.SendBatch(ctx, batch)
	defer func() {
		if err := results.Close(); err != nil {
			u.logger.Error("Failed to close mark-sent batch",
				slog.Int("batch_size", len(tokens)),
				slog.Any("error", err))
		}
	}()
	for i := range tokens {
		if _, err := results.Exec(); err != nil {
			u.logger.Error("Failed to mark outbox item as sent",
				slog.Int64("id", tokens[i].id),
				slog.Any("error", err))
		}
	}
}

func (u *StatusUpdater) markSentTokenIfLocked(ctx context.Context, token outboxLockToken, now time.Time) {
	_, err := u.db.Exec(ctx, markSentIfLockedSQL,
		domain.OutboxStatusSent, now, token.id, domain.OutboxStatusPending, *token.lockedAt)
	if err != nil {
		u.logger.Error("Failed to mark outbox item as sent",
			slog.Int64("id", token.id),
			slog.Any("error", err))
	}
}

func (u *StatusUpdater) markFailed(ctx context.Context, id int64, errMsg string) {
	var item domain.YouTubeNotificationOutbox
	found, err := deliverysql.GetDeliverySQL(ctx, u.db, &item, "fetch outbox item for retry", `
			SELECT id, kind, channel_id, content_id, payload::text AS payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error
		FROM youtube_notification_outbox
		WHERE id = ?
	`, id)
	if err != nil || !found {
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
	found, err := deliverysql.GetDeliverySQL(ctx, u.db, &item, "fetch locked outbox item for retry", `
			SELECT id, kind, channel_id, content_id, payload::text AS payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error
		FROM youtube_notification_outbox
		WHERE id = ? AND status = ? AND locked_at = ?
	`, id, domain.OutboxStatusPending, *lockedAt)
	if err != nil || !found {
		u.logger.Warn("Failed to fetch locked outbox item for retry", slog.Int64("id", id), slog.Any("error", err))
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
	_, err := u.db.Exec(ctx, `
		UPDATE youtube_notification_outbox
		SET status = $1, locked_at = NULL, attempt_count = $2, error = $3
		WHERE id = $4
	`, domain.OutboxStatusFailed, attemptCount, deliverysql.TruncateString(errMsg, 500), id)
	if err != nil {
		u.logger.Error("Failed to mark outbox item as permanently failed",
			slog.Int64("id", id),
			slog.Any("error", err))
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

	tag, err := u.db.Exec(ctx, `
		UPDATE youtube_notification_outbox
		SET status = $1, locked_at = NULL, attempt_count = $2, error = $3
		WHERE id = $4 AND status = $5 AND locked_at = $6
	`, domain.OutboxStatusFailed, attemptCount, deliverysql.TruncateString(errMsg, 500), token.id, domain.OutboxStatusPending, *token.lockedAt)
	if err != nil {
		u.logger.Error("Failed to mark outbox item as permanently failed",
			slog.Int64("id", token.id),
			slog.Any("error", err))
	}
	if tag.RowsAffected() > 0 {
		u.logger.Warn("Outbox item permanently failed after max retries",
			slog.Int64("id", token.id),
			slog.Int("attempts", attemptCount))
	}
}

func (u *StatusUpdater) scheduleFailedRetry(ctx context.Context, id int64, attemptCount int, errMsg string) {
	nextAttempt := time.Now().Add(u.config.RetryBackoff * time.Duration(attemptCount))
	_, err := u.db.Exec(ctx, `
		UPDATE youtube_notification_outbox
		SET locked_at = NULL, attempt_count = $1, next_attempt_at = $2, error = $3
		WHERE id = $4
	`, attemptCount, nextAttempt, deliverysql.TruncateString(errMsg, 500), id)
	if err != nil {
		u.logger.Error("Failed to schedule outbox item for retry",
			slog.Int64("id", id),
			slog.Any("error", err))
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
	tag, err := u.db.Exec(ctx, `
		UPDATE youtube_notification_outbox
		SET locked_at = NULL, attempt_count = $1, next_attempt_at = $2, error = $3
		WHERE id = $4 AND status = $5 AND locked_at = $6
	`, attemptCount, nextAttempt, deliverysql.TruncateString(errMsg, 500), token.id, domain.OutboxStatusPending, *token.lockedAt)
	if err != nil {
		u.logger.Error("Failed to schedule outbox item for retry",
			slog.Int64("id", token.id),
			slog.Any("error", err))
	}
	if tag.RowsAffected() > 0 {
		u.logger.Info("Outbox item scheduled for retry",
			slog.Int64("id", token.id),
			slog.Int("attempt", attemptCount),
			slog.Time("next_attempt", nextAttempt))
	}
}
