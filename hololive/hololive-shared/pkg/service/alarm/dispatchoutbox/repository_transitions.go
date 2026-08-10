package dispatchoutbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

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
// 지우므로 locked_by 일치만으로 소유권이 보장된다(RouteSendingFailures와 동형).
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

func (r *PgxRepository) RouteFailures(ctx context.Context, updates []FailureUpdate, workerID string) error {
	return r.routeFailureUpdates(ctx, updates, workerID, "repository_transitions_0160_06.sql", "route dispatch delivery failures")
}

// RouteSendingFailures는 post-send failure에서 row가 이미 'sending' 상태일 때 쓴다.
// RouteFailures의 AND status='leased' 조건과 달리 AND status IN ('leased','sending')을
// 사용하고, lock_expires_at 조건을 제거한다. locked_by=$2 + status IN('leased','sending')이
// 소유권을 보장하므로 만료된 lease에서도 소유 worker의 보상 전이는 안전하다.
// RecoverExpiredLeased는 'leased'만 접촉하고 'sending'은 QuarantineStaleSending이
// 담당하므로 다른 worker의 선점 경쟁이 없다. terminal 상태는 status 조건으로 보호된다.
func (r *PgxRepository) RouteSendingFailures(ctx context.Context, updates []FailureUpdate, workerID string) error {
	return r.routeFailureUpdates(ctx, updates, workerID, "repository_transitions_0170_07.sql", "route dispatch delivery sending failures")
}

func (r *PgxRepository) RequeuePreSend(ctx context.Context, updates []FailureUpdate, workerID string) error {
	for i := range updates {
		if updates[i].TargetStatus != StatusRetry {
			return fmt.Errorf("requeue pre-send dispatch deliveries: unsupported target status %q for delivery %d", updates[i].TargetStatus, updates[i].ID)
		}
	}
	return r.routeFailureUpdates(ctx, updates, workerID, "repository_transitions_0185_08.sql", "requeue pre-send dispatch deliveries")
}

func (r *PgxRepository) routeFailureUpdates(ctx context.Context, updates []FailureUpdate, workerID, queryFile, action string) error {
	if len(updates) == 0 {
		return nil
	}
	if err := validateFailureUpdates(updates, action); err != nil {
		return err
	}
	applied, err := r.applyFailureUpdates(ctx, updates, workerID, queryFile, action)
	if err != nil {
		return err
	}
	if len(applied) == len(updates) {
		return nil
	}
	return r.partialFailureRoutingError(updates, applied, action)
}

func validateFailureUpdates(updates []FailureUpdate, action string) error {
	for i := range updates {
		if updates[i].TargetStatus != StatusRetry && updates[i].TargetStatus != StatusDLQ {
			return fmt.Errorf("%s: unsupported target status %q for delivery %d", action, updates[i].TargetStatus, updates[i].ID)
		}
	}
	return nil
}

func (r *PgxRepository) applyFailureUpdates(ctx context.Context, updates []FailureUpdate, workerID, queryFile, action string) ([]int64, error) {
	raw, err := json.Marshal(updates)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal batch: %w", action, err)
	}
	rows, err := r.pool.Query(ctx, mustSQL(queryFile), jsonbRecordsetParam(raw), workerID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	applied, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("%s: collect applied ids: %w", action, err)
	}
	return applied, nil
}

func (r *PgxRepository) partialFailureRoutingError(updates []FailureUpdate, applied []int64, action string) error {
	unapplied := unappliedFailureIDs(updates, applied)
	observePGTransitionPartial()
	if r.logger != nil {
		r.logger.Warn("dispatch delivery partial failure routing",
			slog.String("action", action),
			slog.Int("rows_applied", len(applied)),
			slog.Int("rows_expected", len(updates)),
			slog.Any("unapplied_ids", unapplied),
		)
	}
	return &PartialTransitionError{
		Action:       action,
		Updated:      int64(len(applied)),
		Expected:     int64(len(updates)),
		UnappliedIDs: unapplied,
	}
}

func unappliedFailureIDs(updates []FailureUpdate, applied []int64) []int64 {
	appliedSet := make(map[int64]struct{}, len(applied))
	for _, id := range applied {
		appliedSet[id] = struct{}{}
	}
	unapplied := make([]int64, 0, len(updates)-len(applied))
	for i := range updates {
		if _, ok := appliedSet[updates[i].ID]; !ok {
			unapplied = append(unapplied, updates[i].ID)
		}
	}
	return unapplied
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
