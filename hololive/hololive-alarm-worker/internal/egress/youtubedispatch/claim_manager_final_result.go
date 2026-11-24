package youtubedispatch

import (
	"context"
	"fmt"
)

func (d *ClaimManager) logFinalizedCommunityShortsOutboxResults(ctx context.Context, outboxIDs []int64) error {
	if err := d.auditLogger.logFinalizedCommunityShortsOutboxResults(ctx, outboxIDs); err != nil {
		return fmt.Errorf("log finalized community shorts outbox results: %w", err)
	}

	return nil
}
