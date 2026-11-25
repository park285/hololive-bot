package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"
)

func (r *PgxRepository) RecoverExpiredLeased(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("recover expired leased dispatch deliveries: limit must be positive")
	}

	tag, err := pgxdb.Exec(ctx, r.pool, recoverExpiredLeasedSQL, limit, maxDeliveriesPerSendUnit)
	if err != nil {
		return 0, fmt.Errorf("recover expired leased dispatch deliveries: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

func (r *PgxRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	olderThanMS, err := leaseMilliseconds(olderThan)
	if err != nil {
		return 0, fmt.Errorf("quarantine stale sending dispatch deliveries: older than: %w", err)
	}

	tag, err := pgxdb.Exec(ctx, r.pool, quarantineStaleSendingSQL, limit, olderThanMS)
	if err != nil {
		return 0, fmt.Errorf("quarantine stale sending dispatch deliveries: %w", err)
	}

	return int(tag.RowsAffected()), nil
}
