package dispatchoutbox

import (
	"context"
	"fmt"
	"time"
)

func (r *PgxRepository) RecoverExpiredLeased(ctx context.Context, limit int) (int, error) {
	return r.recoverWithQuery(ctx, `
		WITH picked AS (
			SELECT id FROM alarm_dispatch_deliveries
			WHERE status='leased' AND lock_expires_at < NOW()
			ORDER BY lock_expires_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE alarm_dispatch_deliveries d
		SET status='retry',
			next_attempt_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error='lease expired before external send',
			updated_at=NOW()
		FROM picked
		WHERE d.id = picked.id`, limit)
}

func (r *PgxRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	seconds := int(olderThan.Seconds())
	if seconds <= 0 {
		seconds = 300
	}
	return r.recoverWithQuery(ctx, `
		WITH picked AS (
			SELECT id FROM alarm_dispatch_deliveries
			WHERE status='sending'
			  AND sending_started_at < NOW() - ($2::INT * INTERVAL '1 second')
			ORDER BY sending_started_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE alarm_dispatch_deliveries d
		SET status='quarantined',
			quarantined_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error='stale sending; external send outcome unknown',
			updated_at=NOW()
		FROM picked
		WHERE d.id = picked.id`, limit, seconds)
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
