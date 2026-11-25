package targetprojection

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"
)

const projectionRetentionTimeout = 8 * time.Second

type RetentionResult struct {
	LeasesDeleted      int64
	ReasonsDeleted     int64
	TargetsDeleted     int64
	GenerationsDeleted int64
}

func (r *Refresher) Retain(ctx context.Context, now time.Time, age time.Duration, batchSize int) (RetentionResult, error) {
	if r == nil || r.pool == nil || now.IsZero() || age <= 0 || batchSize < 1 || batchSize > 1000 {
		return RetentionResult{}, errors.New("retain youtube target projections: invalid retention request")
	}

	ctx, cancel := context.WithTimeout(ctx, projectionRetentionTimeout)

	defer cancel()

	cutoff := now.UTC().Add(-age)

	leaseTag, err := pgxdb.Exec(ctx, r.pool, deleteRetiredJobLeasesSQL, cutoff, batchSize)
	if err != nil {
		return RetentionResult{}, fmt.Errorf("retain youtube target projections: delete retired job leases: %w", err)
	}

	var result RetentionResult

	result.LeasesDeleted = leaseTag.RowsAffected()

	err = pgxdb.QueryRow(ctx, r.pool, deleteRetiredGenerationsSQL, cutoff, batchSize).
		Scan(&result.ReasonsDeleted, &result.TargetsDeleted, &result.GenerationsDeleted)
	if err != nil {
		return RetentionResult{LeasesDeleted: result.LeasesDeleted}, fmt.Errorf("retain youtube target projections: delete retired generation rows: %w", err)
	}

	return result, nil
}
