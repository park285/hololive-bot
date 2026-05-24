package dispatchoutbox

import (
	"context"
	"fmt"
	"time"

	json "github.com/park285/shared-go/pkg/json"
)

func (r *PgxRepository) MarkSending(ctx context.Context, ids []int64, workerID string, extendLease time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	seconds := int(extendLease.Seconds())
	if seconds <= 0 {
		seconds = 60
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET status='sending',
			sending_started_at=NOW(),
			lock_expires_at=NOW() + ($2::INT * INTERVAL '1 second'),
			updated_at=NOW()
		WHERE id = ANY($1)
		  AND status = 'leased'
		  AND locked_by = $3
		  AND lock_expires_at > NOW()`, ids, seconds, workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries sending: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(ids), "mark dispatch deliveries sending")
}

func (r *PgxRepository) MarkSent(ctx context.Context, ids []int64, workerID string) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET status='sent',
			sent_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			updated_at=NOW()
		WHERE id = ANY($1)
		  AND status = 'sending'
		  AND locked_by = $2
		  AND lock_expires_at > NOW()`, ids, workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries sent: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(ids), "mark dispatch deliveries sent")
}

func (r *PgxRepository) ScheduleRetry(ctx context.Context, updates []RetryUpdate, workerID string) error {
	if len(updates) == 0 {
		return nil
	}
	raw, err := json.Marshal(updates)
	if err != nil {
		return fmt.Errorf("schedule dispatch delivery retries: marshal batch: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		WITH input AS (
			SELECT *
			FROM jsonb_to_recordset($1::jsonb) AS x(
				id BIGINT,
				attempt_count INT,
				next_attempt_at TIMESTAMPTZ,
				error TEXT
			)
		)
			UPDATE alarm_dispatch_deliveries
			SET status='retry',
				attempt_count=input.attempt_count,
				next_attempt_at=input.next_attempt_at,
				locked_by=NULL,
				locked_at=NULL,
				lock_expires_at=NULL,
				last_error=input.error,
				updated_at=NOW()
			FROM input
			WHERE alarm_dispatch_deliveries.id=input.id
			  AND status='leased'
			  AND locked_by=$2
			  AND lock_expires_at > NOW()`, jsonbRecordsetParam(raw), workerID)
	if err != nil {
		return fmt.Errorf("schedule dispatch delivery retries: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(updates), "schedule dispatch delivery retries")
}

func (r *PgxRepository) MoveToDLQ(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, updates, StatusDLQ, workerID)
}

func (r *PgxRepository) Quarantine(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, updates, StatusQuarantined, workerID)
}

func (r *PgxRepository) ReleaseLeased(ctx context.Context, ids []int64, workerID string) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE alarm_dispatch_deliveries
		SET status='retry',
			next_attempt_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error='lease released before external send',
			updated_at=NOW()
		WHERE id = ANY($1)
		  AND status = 'leased'
		  AND locked_by = $2`, ids, workerID)
	if err != nil {
		return fmt.Errorf("release dispatch deliveries: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(ids), "release dispatch deliveries")
}
