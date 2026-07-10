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

// MarkSent의 fence는 status='sending' AND locked_by만 본다. 그룹 발송이 lease를
// 초과해도 성공 발송은 sent로 확정돼야 하고(P0-2: lease 조건이 있으면 quarantine →
// replay 중복 발송), QuarantineStaleSending이 'sending' 회수 시 locked_by를 NULL로
// 지우므로 locked_by 일치만으로 소유권이 보장된다(ScheduleSendingRetry와 동형).
func (r *PgxRepository) MarkSent(ctx context.Context, ids []int64, workerID string) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := r.pool.Exec(ctx, mustSQL("repository_transitions_0040_02.sql"), ids, workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries sent: %w", err)
	}
	return validatePostSendRowsAffected(tag.RowsAffected(), len(ids), "mark dispatch deliveries sent", r.logger)
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

// validatePostSendRowsAffected는 외부 발송 뒤 ownership 변경을 관측 가능한 오류로
// 반환한다. 호출자는 이미 완료된 외부 발송을 retry로 되돌리지 않고 오류만 보고해야 한다.
func validatePostSendRowsAffected(got int64, want int, action string, logger *slog.Logger) error {
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
	return &PartialTransitionError{Action: action, Updated: got, Expected: int64(want)}
}
