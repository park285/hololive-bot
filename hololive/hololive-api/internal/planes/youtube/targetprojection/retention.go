package targetprojection

import (
	"context"
	"fmt"
	"time"
)

type RetentionResult struct {
	LeasesDeleted      int64
	GenerationsDeleted int64
}

func (r *Refresher) Retain(ctx context.Context, now time.Time, age time.Duration, batchSize int) (RetentionResult, error) {
	if r == nil || r.pool == nil || now.IsZero() || age <= 0 || batchSize < 1 || batchSize > 1000 {
		return RetentionResult{}, fmt.Errorf("retain youtube target projections: invalid retention request")
	}
	cutoff := now.UTC().Add(-age)
	leaseTag, err := r.pool.Exec(ctx, mustSQL("delete_retired_job_leases.sql"), cutoff, batchSize)
	if err != nil {
		return RetentionResult{}, fmt.Errorf("retain youtube target projections: delete retired job leases: %w", err)
	}
	generationTag, err := r.pool.Exec(ctx, mustSQL("delete_retired_generations.sql"), cutoff, batchSize)
	if err != nil {
		return RetentionResult{LeasesDeleted: leaseTag.RowsAffected()}, fmt.Errorf("retain youtube target projections: delete retired generations: %w", err)
	}
	return RetentionResult{
		LeasesDeleted:      leaseTag.RowsAffected(),
		GenerationsDeleted: generationTag.RowsAffected(),
	}, nil
}
