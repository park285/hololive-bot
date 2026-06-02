package dispatchoutbox

import (
	"context"
	"fmt"
	"log/slog"
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
	return warnRowsAffected(tag.RowsAffected(), len(ids), "mark dispatch deliveries sending", r.logger)
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
	return warnRowsAffected(tag.RowsAffected(), len(ids), "mark dispatch deliveries sent", r.logger)
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

// ScheduleSendingRetry는 post-send retryable failure(502/503)에서 row가 이미
// 'sending' 상태일 때 retry로 전환한다. ScheduleRetry의 AND status='leased' 조건과
// 달리 AND status IN ('leased','sending')을 사용하고, lock_expires_at 조건을 제거한다.
// locked_by=$2 + status IN('leased','sending')이 소유권을 보장하므로 만료된 lease에서도
// 소유 worker의 reschedule은 안전하다. RecoverExpiredLeased는 'leased'만 접촉하고
// 'sending'은 QuarantineStaleSending이 담당하므로 다른 worker의 선점 경쟁이 없다.
// quarantined/dlq/sent 같은 terminal 상태는 status 조건으로 보호된다.
func (r *PgxRepository) ScheduleSendingRetry(ctx context.Context, updates []RetryUpdate, workerID string) error {
	if len(updates) == 0 {
		return nil
	}
	raw, err := json.Marshal(updates)
	if err != nil {
		return fmt.Errorf("schedule dispatch delivery sending retries: marshal batch: %w", err)
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
			  AND status IN ('leased','sending')
			  AND locked_by=$2`, jsonbRecordsetParam(raw), workerID)
	if err != nil {
		return fmt.Errorf("schedule dispatch delivery sending retries: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(updates), "schedule dispatch delivery sending retries")
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

// warnRowsAffected는 MarkSent/MarkSending에서 concurrent workers가 partial update를
// 일으킬 때 error 대신 warn을 로그하고 metric을 emit한다.
// error를 반환하지 않는 이유: QuarantineStaleSending/RecoverExpiredLeased가 잔여 row를
// 처리하며, Iris reply admission store(client_request_id 기반)가 KakaoTalk 레벨 중복을
// 차단하므로 partial update는 silent alarm loss가 아니라 정상 경합이다.
func warnRowsAffected(got int64, want int, action string, logger *slog.Logger) error {
	if got == int64(want) {
		return nil
	}
	observePGTransitionPartial()
	if logger != nil {
		logger.Warn("dispatch delivery partial update",
			slog.String("action", action),
			slog.Int64("rows_affected", got),
			slog.Int("rows_expected", want),
		)
	}
	return nil
}
