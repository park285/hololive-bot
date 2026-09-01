package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

const outboxCleanupBatchSize = 1000

func (d *ClaimManager) cleanupOutbox(ctx context.Context) {
	if d == nil || d.db == nil || d.transition == nil {
		return
	}

	outboxCutoff := time.Now().UTC().Add(-d.config.CleanupAfter)

	var (
		cursor  store.CleanupCursor
		deleted int
		guarded int
	)

	for {
		result, err := d.transition.CleanupTerminalOutboxes(ctx, outboxCutoff, cursor, outboxCleanupBatchSize)
		if err != nil {
			d.logger.Warn("Failed to cleanup ledger-backed terminal outbox items", slog.Any("error", err))

			return
		}

		observeCleanupResult(result)

		deleted += result.DeletedOutboxes
		guarded += result.GuardedOutboxes

		if result.ExaminedOutboxes < outboxCleanupBatchSize {
			break
		}

		cursor = result.NextCursor

		if err := deliverysql.YieldBetweenDeleteBatches(ctx); err != nil {
			d.logger.Warn("Failed to yield between terminal outbox cleanup batches", slog.Any("error", err))

			return
		}
	}

	if deleted > 0 || guarded > 0 {
		d.logger.Info("Completed ledger-backed terminal outbox cleanup scan",
			slog.Int("deleted", deleted),
			slog.Int("guarded", guarded))
	}

	d.cleanupExpiredFanoutOutboxes(ctx)
}

// cutoff가 max(CleanupAfter, ClaimFreshnessWindow)인 이유: CleanupAfter >= ClaimFreshnessWindow가
// config invariant로 보장되지 않으므로, max로 삭제 대상을 항상 created_at < now-ClaimFreshnessWindow로
// 묶어 primary claim(dispatcher_claim.go의 created_at >= now-ClaimFreshnessWindow)에서 다시 claim될 수
// 없음을 보장한다. ClaimFreshnessWindow<=0이면 claim에 신선도 하한이 없어 안전한 cutoff가 없으므로 skip.
// NOT EXISTS delivery 가드는 ON DELETE CASCADE로 인한 delivery/telemetry 동반 삭제를 막는다.
func (d *ClaimManager) cleanupExpiredFanoutOutboxes(ctx context.Context) {
	if d.config.ClaimFreshnessWindow <= 0 {
		return
	}

	now := time.Now().UTC()
	pendingCutoff := now.Add(-d.orphanPendingCutoff())
	lockExpiry := now.Add(-d.config.LockTimeout)

	var total int

	for {
		deleted, err := d.transition.CleanupExpiredFanoutOutboxes(ctx, pendingCutoff, lockExpiry, outboxCleanupBatchSize)
		if err != nil {
			d.logger.Warn("Failed to cleanup expired fanout outbox items", slog.Any("error", err))

			return
		}

		total += deleted
		if deleted < outboxCleanupBatchSize {
			break
		}

		if err := deliverysql.YieldBetweenDeleteBatches(ctx); err != nil {
			d.logger.Warn("Failed to yield between expired fanout cleanup batches", slog.Any("error", err))

			return
		}
	}

	if total > 0 {
		d.logger.Info("Cleaned up expired fanout outbox items", slog.Int("deleted", total))
	}
}

func (d *ClaimManager) orphanPendingCutoff() time.Duration {
	return max(d.config.CleanupAfter, d.config.ClaimFreshnessWindow)
}

func (d *ClaimManager) quarantineStaleSendingDeliveries(ctx context.Context) {
	if d == nil || d.transition == nil {
		return
	}

	d.quarantineStaleLogicalGroups(ctx)
}

func (d *ClaimManager) quarantineStaleLogicalGroups(ctx context.Context) {
	result, err := d.transition.QuarantineStaleLogicalGroups(ctx, d.config.BatchSize)
	observeLifecycleApply("quarantine_logical_group", result.ApplyResult, err, max(result.QuarantinedDeliveries, 1))

	if err != nil {
		d.logger.Warn("Failed to quarantine stale logical delivery groups", slog.Any("error", err))

		return
	}

	for i := range result.Blocked {
		d.logger.Error("Blocked stale logical delivery group quarantine",
			slog.String("logical_key_hash", result.Blocked[i].KeyHash),
			slog.String("invariant_reason", string(result.Blocked[i].Reason)))
	}

	if result.Outcome != store.ApplyApplied || result.QuarantinedDeliveries == 0 {
		return
	}

	observeLedgerOperation("record_quarantined", result.ApplyResult, result.QuarantinedDeliveries)

	if err := d.projector.Project(ctx, result.TouchedOutboxIDs); err != nil {
		d.logger.Warn("Failed to update outbox statuses after logical group quarantine", slog.Any("error", err))

		return
	}

	if err := d.logFinalizedCommunityShortsOutboxResults(ctx, result.TouchedOutboxIDs); err != nil {
		d.logger.Warn("Failed to log finalized community/shorts outbox results after logical group quarantine", slog.Any("error", err))
	}

	d.logger.Warn("Quarantined stale logical delivery groups",
		slog.Int("delivery_count", result.QuarantinedDeliveries),
		slog.Int("outbox_count", len(result.TouchedOutboxIDs)),
		slog.Duration("older_than", d.config.LockTimeout))
}

func (d *ClaimManager) releaseDeliveryClaims(ctx context.Context, claims []dispatchstate.ClaimToken) error {
	if d == nil || d.db == nil || len(claims) == 0 {
		return nil
	}

	repository := observation.NewRepositoryContext(ctx, d.db)

	for i := range claims {
		if _, err := repository.ReleaseAlarmStateClaim(ctx, claims[i].Kind, claims[i].PostID, claims[i].AuthorizedAt); err != nil {
			return fmt.Errorf("release claim at index %d: %w", i, err)
		}
	}

	return nil
}

func (d *ClaimManager) releaseDeliveryClaimsWithWarning(
	ctx context.Context,
	claims []dispatchstate.ClaimToken,
	message string,
	attrs ...any,
) {
	if releaseErr := d.releaseDeliveryClaims(ctx, claims); releaseErr != nil && d.logger != nil {
		d.logger.Warn(message, append(attrs, slog.Any("error", releaseErr))...)
	}
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
