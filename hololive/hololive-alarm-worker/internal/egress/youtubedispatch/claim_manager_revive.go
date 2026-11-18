package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
)

func (d *ClaimManager) reviveStaleFailedOutbox(ctx context.Context, freshnessWindow time.Duration, batchSize int) (int64, error) {
	deliveryResult, err := d.transition.ReviveFailedLogicalGroups(ctx, freshnessWindow, batchSize)
	observeLifecycleApply("revive_logical_group", deliveryResult.ApplyResult, err, max(deliveryResult.RevivedLogicalGroups, 1))

	if err != nil {
		observeOutboxReviveError()

		return 0, fmt.Errorf("revive failed lifecycle groups: %w", err)
	}

	for i := range deliveryResult.Blocked {
		d.logger.Error("Blocked failed logical delivery group revive",
			slog.String("logical_key_hash", deliveryResult.Blocked[i].KeyHash),
			slog.String("invariant_reason", string(deliveryResult.Blocked[i].Reason)))
	}

	if deliveryResult.Outcome != store.ApplyApplied {
		return 0, fmt.Errorf("revive failed lifecycle groups: outcome %s", deliveryResult.Outcome)
	}

	fanoutResult, fanoutCount, err := d.transition.ReviveFailedFanoutOutboxes(ctx, freshnessWindow, batchSize)
	observeLifecycleApply("revive_fanout", fanoutResult, err, max(fanoutCount, 1))

	if err != nil {
		observeOutboxReviveError()

		return 0, fmt.Errorf("revive failed fanout outboxes: %w", err)
	}

	if fanoutResult.Outcome != store.ApplyApplied {
		return 0, fmt.Errorf("revive failed fanout outboxes: outcome %s", fanoutResult.Outcome)
	}

	touched := slices.Concat(deliveryResult.TouchedOutboxIDs, fanoutResult.TouchedOutboxIDs)
	if err := d.projector.Project(ctx, touched); err != nil {
		d.logger.Warn("Immediate aggregate projection failed after logical group revive", slog.Any("error", err))
	}

	revived := int64(deliveryResult.RevivedLogicalGroups + fanoutCount)
	if revived > 0 {
		observeOutboxRevived(revived)
	}

	return revived, nil
}
