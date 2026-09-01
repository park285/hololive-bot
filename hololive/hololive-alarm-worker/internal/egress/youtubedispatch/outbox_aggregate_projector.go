package youtubedispatch

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
)

type OutboxAggregateProjector struct {
	delivery *store.DeliveryRepository
}

func newOutboxAggregateProjector(delivery *store.DeliveryRepository) *OutboxAggregateProjector {
	return &OutboxAggregateProjector{delivery: delivery}
}

func (p *OutboxAggregateProjector) Project(ctx context.Context, outboxIDs []int64) error {
	if p == nil || p.delivery == nil || len(outboxIDs) == 0 {
		return nil
	}

	startedAt := time.Now()

	if err := p.delivery.UpdateOutboxAggregateStatuses(ctx, outboxIDs); err != nil {
		observeAggregateProjection("error", time.Since(startedAt))

		return fmt.Errorf("project outbox aggregate statuses: %w", err)
	}

	observeAggregateProjection("applied", time.Since(startedAt))

	return nil
}

func (p *OutboxAggregateProjector) ProjectPending(ctx context.Context, batchSize int) ([]int64, error) {
	if p == nil || p.delivery == nil {
		return nil, nil
	}

	outboxIDs, err := p.delivery.FindPendingOutboxIDsForAggregateSync(ctx, batchSize)
	if err != nil {
		return nil, fmt.Errorf("find pending outbox aggregate projections: %w", err)
	}

	if err := p.Project(ctx, outboxIDs); err != nil {
		return nil, fmt.Errorf("project pending outbox aggregates: %w", err)
	}

	return outboxIDs, nil
}
