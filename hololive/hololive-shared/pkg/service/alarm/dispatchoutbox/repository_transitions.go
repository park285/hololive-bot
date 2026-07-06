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
	tag, err := r.pool.Exec(ctx, mustSQL("repository_transitions_0020_01.sql"), ids, seconds, workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries sending: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(ids), "mark dispatch deliveries sending")
}

func (r *PgxRepository) MarkSent(ctx context.Context, ids []int64, workerID string) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := r.pool.Exec(ctx, mustSQL("repository_transitions_0040_02.sql"), ids, workerID)
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
	tag, err := r.pool.Exec(ctx, mustSQL("repository_transitions_0066_03.sql"), jsonbRecordsetParam(raw), workerID)
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
	tag, err := r.pool.Exec(ctx, mustSQL("repository_transitions_0111_04.sql"), jsonbRecordsetParam(raw), workerID)
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
	tag, err := r.pool.Exec(ctx, mustSQL("repository_transitions_0152_05.sql"), ids, workerID)
	if err != nil {
		return fmt.Errorf("release dispatch deliveries: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(ids), "release dispatch deliveries")
}

// warnRowsAffected는 MarkSent에서 concurrent workers가 partial update를
// 일으킬 때 error 대신 warn을 로그하고 metric을 emit한다.
// MarkSending은 외부 전송 전 소유권 gate라 partial update를 error로 반환해야 한다.
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
