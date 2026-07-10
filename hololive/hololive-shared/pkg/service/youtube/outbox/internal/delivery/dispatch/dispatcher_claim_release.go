package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/telemetry"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func (d *ClaimManager) releaseOutboxLock(ctx context.Context, id int64, lockedAt *time.Time) {
	query := mustSQL("dispatcher_claim_release_0019_01.sql")
	args := []any{id, domain.OutboxStatusPending}
	if lockedAt != nil {
		query += " AND locked_at = ?"
		args = append(args, *lockedAt)
	}
	if _, err := deliverysql.ExecDeliverySQL(ctx, d.db, "release outbox lock", query, args...); err != nil {
		d.logger.Warn("Failed to release outbox lock",
			slog.Int64("id", id),
			slog.Any("error", err))
	}
}

const outboxCleanupBatchSize = 1000

func (d *ClaimManager) cleanupOutbox(ctx context.Context) {
	if d == nil || d.db == nil {
		return
	}

	outboxCutoff := time.Now().UTC().Add(-d.config.CleanupAfter)
	deleted, err := d.deleteTerminalOutboxBatches(ctx, outboxCutoff, outboxCleanupBatchSize)
	if err != nil {
		d.logger.Warn("Failed to cleanup old outbox items", slog.Any("error", err))
		return
	}

	if deleted > 0 {
		d.logger.Info("Cleaned up old outbox items", slog.Int64("deleted", deleted))
	}

	d.cleanupOrphanPendingOutbox(ctx)
}

// 무제한 단문 DELETE는 youtube_notification_delivery ON DELETE CASCADE 증폭으로
// 락 보유 시간이 길어진다 — picked LIMIT 배치 루프(retention.go 패턴)를 유지할 것.
func (d *ClaimManager) deleteTerminalOutboxBatches(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	var total int64
	for {
		deleted, err := deliverysql.ExecDeliverySQL(ctx, d.db, "cleanup old outbox items", mustSQL("dispatcher_claim_release_0042_02.sql"), domain.OutboxStatusSent, domain.OutboxStatusFailed, cutoff, batchSize)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < int64(batchSize) {
			return total, nil
		}
		if err := deliverysql.YieldBetweenDeleteBatches(ctx); err != nil {
			return total, err
		}
	}
}

// cutoff가 max(CleanupAfter, ClaimFreshnessWindow)인 이유: CleanupAfter >= ClaimFreshnessWindow가
// config invariant로 보장되지 않으므로, max로 삭제 대상을 항상 created_at < now-ClaimFreshnessWindow로
// 묶어 primary claim(dispatcher_claim.go의 created_at >= now-ClaimFreshnessWindow)에서 다시 claim될 수
// 없음을 보장한다. ClaimFreshnessWindow<=0이면 claim에 신선도 하한이 없어 안전한 cutoff가 없으므로 skip.
// NOT EXISTS delivery 가드는 ON DELETE CASCADE로 인한 delivery/telemetry 동반 삭제를 막는다.
func (d *ClaimManager) cleanupOrphanPendingOutbox(ctx context.Context) {
	if d.config.ClaimFreshnessWindow <= 0 {
		return
	}

	now := time.Now().UTC()
	pendingCutoff := now.Add(-d.orphanPendingCutoff())
	lockExpiry := now.Add(-d.config.LockTimeout)

	deleted, err := deliverysql.ExecDeliverySQL(ctx, d.db, "cleanup orphan pending outbox items", mustSQL("dispatcher_claim_release_0072_03.sql"), domain.OutboxStatusPending, pendingCutoff, lockExpiry)
	if err != nil {
		d.logger.Warn("Failed to cleanup orphan pending outbox items", slog.Any("error", err))
		return
	}

	if deleted > 0 {
		d.logger.Info("Cleaned up orphan pending outbox items", slog.Int64("deleted", deleted))
	}
}

func (d *ClaimManager) orphanPendingCutoff() time.Duration {
	return max(d.config.CleanupAfter, d.config.ClaimFreshnessWindow)
}

func (d *ClaimManager) quarantineStaleSendingDeliveries(ctx context.Context) {
	if d == nil || d.delivery == nil {
		return
	}

	outboxIDs, quarantined, err := d.delivery.QuarantineStaleSending(ctx, d.config.LockTimeout, d.config.BatchSize)
	if err != nil {
		d.logger.Warn("Failed to quarantine stale sending delivery rows", slog.Any("error", err))
		return
	}
	if quarantined == 0 {
		return
	}

	if err := d.delivery.UpdateOutboxAggregateStatuses(ctx, outboxIDs); err != nil {
		d.logger.Warn("Failed to update outbox statuses after stale sending quarantine", slog.Any("error", err))
		return
	}
	if err := d.logFinalizedCommunityShortsOutboxResults(ctx, outboxIDs); err != nil {
		d.logger.Warn("Failed to log finalized community/shorts outbox results after stale sending quarantine", slog.Any("error", err))
	}

	d.logger.Warn("Quarantined stale sending delivery rows",
		slog.Int("delivery_count", quarantined),
		slog.Int("outbox_count", len(outboxIDs)),
		slog.Duration("older_than", d.config.LockTimeout))
}

func (d *ClaimManager) releaseDeliveryClaims(ctx context.Context, claims []dispatchstate.ClaimToken) error {
	if d == nil || d.db == nil || len(claims) == 0 {
		return nil
	}

	repository := trackingrepo.NewRepository(d.db)
	for i := range claims {
		if _, err := repository.ReleaseAlarmStateClaim(ctx, claims[i].Kind, claims[i].PostID, claims[i].AuthorizedAt); err != nil {
			return fmt.Errorf("release claim at index %d: %w", i, err)
		}
	}
	return nil
}

func (d *ClaimManager) deliveryClaimTimeout() time.Duration {
	claimTimeout := maxCommunityShortsClaimHold
	if d != nil && d.config.LockTimeout > 0 && d.config.LockTimeout < claimTimeout {
		claimTimeout = d.config.LockTimeout
	}
	if claimTimeout <= 0 {
		return maxCommunityShortsClaimHold
	}
	return claimTimeout
}

func (d *ClaimManager) logClaimIssue(
	message string,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	level slog.Level,
	attrs ...any,
) {
	if d == nil || d.logger == nil {
		return
	}

	baseAttrs := deliveryClaimLogAttrs(row, outbox, attrs...)
	logClaimIssueAtLevel(d.logger, level, message, baseAttrs...)
}

func deliveryClaimLogAttrs(
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	attrs ...any,
) []any {
	baseAttrs := make([]any, 0, 7+len(attrs))
	baseAttrs = append(baseAttrs,
		slog.Int64(logschema.FieldDeliveryID, row.ID),
		slog.Int64(logschema.FieldOutboxID, outbox.ID),
		slog.String(logschema.FieldRoomID, row.RoomID),
		slog.String(logschema.FieldChannelID, outbox.ChannelID),
		slog.String(deliveryAuditPostIDLogField, telemetry.ResolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload)),
		slog.String(deliveryAuditContentIDLogField, strings.TrimSpace(outbox.ContentID)),
		slog.String(deliveryAuditAlarmTypeLogField, string(outbox.Kind.ToAlarmType())),
	)
	return append(baseAttrs, attrs...)
}

func logClaimIssueAtLevel(logger *slog.Logger, level slog.Level, message string, attrs ...any) {
	switch level {
	case slog.LevelDebug, slog.LevelInfo:
		logger.Info(message, attrs...)
	case slog.LevelWarn:
		logger.Warn(message, attrs...)
	case slog.LevelError:
		logger.Error(message, attrs...)
	default:
		logger.Info(message, attrs...)
	}
}
