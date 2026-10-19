package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"time"
)

// leaseMilliseconds는 SQL에 넘길 밀리초 정수를 만든다. 1ms 미만은 설정 오류라 기본값으로 바꾸지 않고 오류로
// 돌려준다. 모든 claim·발송 진입·stale 격리가 같은 단위를 쓰도록 이 변환을 함께 쓴다.
func leaseMilliseconds(d time.Duration) (int64, error) {
	if d < time.Millisecond {
		return 0, fmt.Errorf("duration must be at least 1ms: %s", d)
	}

	return d.Milliseconds(), nil
}

// MarkSent는 외부 발송 성공을 정산한다. 그룹 발송이 lease를 넘겨도 성공 발송은 sent로 확정해야 하므로(lease 조건이
// 있으면 격리 뒤 재발송 중복) lease 만료는 보지 않는다. 입력 전원이 sent가 되면 같은 문장에서 단위에 Iris
// requestId와 첫 handoff 확인 시각(firstCheck 뒤)을 기록한다. 미스는 외부 성공을 되돌리지 않고 *sendfence.MissError로
// 보고하며, 그때 단위의 handoff 확인은 예약되지 않고 행은 stale 격리·회수 경로가 소유한다.
func (r *PgxRepository) MarkSent(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, irisRequestID string, firstCheck time.Duration) error {
	if len(claims) == 0 {
		return nil
	}

	if irisRequestID == "" {
		return fmt.Errorf("%s: iris request id is required", fenceActionMarkSent)
	}

	firstCheckMS, err := leaseMilliseconds(firstCheck)
	if err != nil {
		return fmt.Errorf("%s: first handoff check: %w", fenceActionMarkSent, err)
	}

	raw, err := jsonv2.Marshal(claims)
	if err != nil {
		return fmt.Errorf("%s: marshal claims: %w", fenceActionMarkSent, err)
	}

	return r.applyFenced(ctx, r.pool, fenceActionMarkSent, sqlMarkSent, fencedTargetIDs(claims),
		jsonbRecordsetParam(raw), workerID, unitID, irisRequestID, firstCheckMS)
}

// ReleaseLeased는 발송 전 claim을 같은 attempt의 retry로 반환한다.
func (r *PgxRepository) ReleaseLeased(ctx context.Context, claims []DeliveryClaim, workerID string) error {
	return r.applyClaims(ctx, fenceActionReleaseLeased, sqlReleaseLeased, claims, workerID)
}

func (r *PgxRepository) applyClaims(ctx context.Context, action, query string, claims []DeliveryClaim, workerID string) error {
	if len(claims) == 0 {
		return nil
	}

	raw, err := jsonv2.Marshal(claims)
	if err != nil {
		return fmt.Errorf("%s: marshal claims: %w", action, err)
	}

	return r.applyFenced(ctx, r.pool, action, query, fencedTargetIDs(claims), jsonbRecordsetParam(raw), workerID)
}

// RouteFailures는 발송 전 실패를 retry·dlq로 정산한다. 잠금 뒤 clock 기준으로 lease가 유효한 소유 claim만 바꾼다.
func (r *PgxRepository) RouteFailures(ctx context.Context, updates []FailureUpdate, workerID string) error {
	return r.routeFailureUpdates(ctx, updates, workerID, sqlRouteFailures, fenceActionRouteFailures)
}

// RouteSendingFailures는 외부 호출 뒤 실패를 retry·dlq로 정산한다. 상태는 leased·sending을 받고 lease 만료는 보지 않는다.
// 만료 회수는 leased만, stale 격리는 sending의 locked_by를 비우므로 소유자·attempt 일치가 소유권 증명이다.
func (r *PgxRepository) RouteSendingFailures(ctx context.Context, updates []FailureUpdate, workerID string) error {
	return r.routeFailureUpdates(ctx, updates, workerID, sqlRouteSendingFailures, fenceActionRouteSendingFailures)
}

// RequeuePreSend는 외부 호출 전에 멈춘 claim을 같은 attempt의 retry로 되돌린다.
func (r *PgxRepository) RequeuePreSend(ctx context.Context, updates []FailureUpdate, workerID string) error {
	for i := range updates {
		if updates[i].TargetStatus != StatusRetry {
			return fmt.Errorf("%s: unsupported target status %q for delivery %d", fenceActionRequeuePreSend, updates[i].TargetStatus, updates[i].ID)
		}
	}

	return r.routeFailureUpdates(ctx, updates, workerID, sqlRequeuePreSend, fenceActionRequeuePreSend)
}

func (r *PgxRepository) routeFailureUpdates(ctx context.Context, updates []FailureUpdate, workerID, query, action string) error {
	if len(updates) == 0 {
		return nil
	}

	if err := validateFailureUpdates(updates, action); err != nil {
		return err
	}

	raw, err := jsonv2.Marshal(prepareFailureUpdates(updates))
	if err != nil {
		return fmt.Errorf("%s: marshal batch: %w", action, err)
	}

	return r.applyFenced(ctx, r.pool, action, query, fencedTargetIDs(updates), jsonbRecordsetParam(raw), workerID)
}

func validateFailureUpdates(updates []FailureUpdate, action string) error {
	seen := make(map[int64]struct{}, len(updates))
	for i := range updates {
		if _, duplicate := seen[updates[i].ID]; duplicate {
			return fmt.Errorf("%s: duplicate delivery %d", action, updates[i].ID)
		}

		seen[updates[i].ID] = struct{}{}
		if updates[i].TargetStatus != StatusRetry && updates[i].TargetStatus != StatusDLQ {
			return fmt.Errorf("%s: unsupported target status %q for delivery %d", action, updates[i].TargetStatus, updates[i].ID)
		}

		if updates[i].RetryAfter < 0 {
			return fmt.Errorf("%s: negative retry delay %s for delivery %d", action, updates[i].RetryAfter, updates[i].ID)
		}
	}

	return nil
}

type failureRouteInput struct {
	*FailureUpdate

	RetryAfterMS *int64 `json:"retry_after_ms,omitempty"`
	MarkDLQ      bool   `json:"mark_dlq,omitzero"`
}

// 상태에 따른 필드 갱신 정책은 코드에서 정한다. SQL은 잠금 뒤 clock에 retry_after_ms를 더하는 저장 연산만 한다.
// 입력에 retry_after_ms가 없는 dlq 행은 next_attempt_at을 그대로 둔다.
func prepareFailureUpdates(updates []FailureUpdate) []failureRouteInput {
	inputs := make([]failureRouteInput, len(updates))
	for i := range updates {
		update := &updates[i]

		inputs[i] = failureRouteInput{FailureUpdate: update, MarkDLQ: update.TargetStatus == StatusDLQ}

		if update.TargetStatus == StatusRetry {
			inputs[i].RetryAfterMS = new(update.RetryAfter.Milliseconds())
		}
	}

	return inputs
}

// MoveToDLQ는 소유한 leased·sending claim을 같은 attempt에서 dlq로 끝낸다.
func (r *PgxRepository) MoveToDLQ(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, fenceActionMoveToDLQ, sqlMoveToDLQ, updates, workerID)
}

// Quarantine은 소유한 leased·sending claim을 같은 attempt에서 quarantined로 끝낸다.
func (r *PgxRepository) Quarantine(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, fenceActionQuarantine, sqlQuarantine, updates, workerID)
}

func (r *PgxRepository) terminalUpdates(ctx context.Context, action, query string, updates []TerminalUpdate, workerID string) error {
	if len(updates) == 0 {
		return nil
	}

	raw, err := jsonv2.Marshal(updates)
	if err != nil {
		return fmt.Errorf("%s: marshal batch: %w", action, err)
	}

	return r.applyFenced(ctx, r.pool, action, query, fencedTargetIDs(updates), jsonbRecordsetParam(raw), workerID)
}
