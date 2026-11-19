package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
)

const projectionRetentionMaxBatches = 64

func (r *Runtime) retainProjectionBatches(ctx context.Context, now time.Time) (targetprojection.RetentionResult, error) {
	var total targetprojection.RetentionResult

	for range projectionRetentionMaxBatches {
		if err := ctx.Err(); err != nil {
			return total, fmt.Errorf("projection retention budget: %w", err)
		}

		part, err := r.projectionRetainer.Retain(ctx, now, r.Config.Retention.ProjectionRetiredAge, r.Config.Retention.BatchSize)

		total.LeasesDeleted += part.LeasesDeleted
		total.ReasonsDeleted += part.ReasonsDeleted
		total.TargetsDeleted += part.TargetsDeleted
		total.GenerationsDeleted += part.GenerationsDeleted

		if err != nil {
			return total, fmt.Errorf("retain projection batch: %w", err)
		}

		if part == (targetprojection.RetentionResult{}) {
			return total, nil
		}
	}

	return total, nil
}
