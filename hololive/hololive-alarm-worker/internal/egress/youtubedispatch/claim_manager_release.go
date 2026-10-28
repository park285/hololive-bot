package youtubedispatch

import (
	"context"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
)

const outboxCleanupBatchSize = 1000

func (d *ClaimManager) cleanupOutbox(ctx context.Context) {
	d.expirePendingDeliveries(ctx)

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

	if _, err := d.transition.CleanupOrphanRequests(ctx, outboxCutoff, outboxCleanupBatchSize); err != nil {
		d.logger.Warn("Failed to cleanup orphan requests", slog.Any("error", err))
	}
}

// 보존 기간이 max(CleanupAfter, ClaimFreshnessWindow)인 이유: CleanupAfter >= ClaimFreshnessWindow가 config invariant로
// 보장되지 않으므로, 삭제 대상을 항상 fanout claim의 신선도 하한(DB 시계 - ClaimFreshnessWindow)보다 오래된 outbox로
// 묶어 다시 claim될 수 없게 한다. 보존 기간과 lease 만료는 claim과 같은 DB 시계로 판정한다.
// ClaimFreshnessWindow<=0이면 claim에 신선도 하한이 없어 안전한 기준이 없으므로 건너뛴다.
// NOT EXISTS delivery 가드는 ON DELETE CASCADE로 인한 delivery/telemetry 동반 삭제를 막는다.
func (d *ClaimManager) cleanupExpiredFanoutOutboxes(ctx context.Context) {
	if d.config.ClaimFreshnessWindow <= 0 {
		return
	}

	var total int

	for {
		deleted, err := d.transition.CleanupExpiredFanoutOutboxes(ctx, d.orphanPendingCutoff(), outboxCleanupBatchSize)
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

func (d *ClaimManager) expirePendingDeliveries(ctx context.Context) {
	var cursor int64

	for {
		result, err := d.transition.ExpirePending(ctx, cursor, d.config.BatchSize)
		if err != nil {
			d.logger.Warn("Failed to expire pending deliveries", slog.Any("error", err))

			return
		}

		// 만료와 기존 논리 결과 정산은 별도다. 확정 commit 뒤 projection보다 먼저 기록한다.
		if result.Outcome == store.ApplyApplied {
			for range result.StaleExpired {
				d.workerTotals.RecordDiscard(workercontract.DiscardStale)
			}
		}

		for _, blocked := range result.Blocked {
			d.logger.Error("Blocked pending delivery expiry", slog.String("logical_key_hash", blocked.KeyHash), slog.String("invariant_reason", string(blocked.Reason)))
		}

		if err := d.projector.Project(ctx, result.TouchedOutboxIDs); err != nil {
			d.logger.Warn("Failed to project expired pending deliveries", slog.Any("error", err))

			return
		}

		if result.Examined < d.config.BatchSize {
			return
		}

		cursor = result.NextID

		if err := deliverysql.YieldBetweenDeleteBatches(ctx); err != nil {
			return
		}
	}
}
