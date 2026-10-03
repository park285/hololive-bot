package dispatchoutbox

import (
	"context"
	"fmt"
	"time"
)

func (r *PgxRepository) RecoverExpiredLeased(ctx context.Context, limit int) (int, error) {
	out, err := r.recoverWithQuery(ctx, mustSQL("repository_maintenance_0010_01.sql"), limit)
	if err != nil {
		return out, fmt.Errorf("recover with query: %w", err)
	}

	return out, nil
}

func (r *PgxRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	seconds := int(olderThan.Seconds())
	if seconds <= 0 {
		seconds = 300
	}

	out, err := r.recoverWithQuery(ctx, mustSQL("repository_maintenance_0035_02.sql"), limit, seconds)
	if err != nil {
		return out, fmt.Errorf("recover with query: %w", err)
	}

	return out, nil
}

func (r *PgxRepository) recoverWithQuery(ctx context.Context, query string, args ...any) (int, error) {
	if len(args) == 0 {
		args = append(args, 100)
	}

	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("recover dispatch deliveries: %w", err)
	}

	return int(tag.RowsAffected()), nil
}
