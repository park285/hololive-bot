package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffloop"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

// HandoffConsumer는 send unit handoff 확인의 claim과 적용이다. 적용의 fence 미스는 *sendfence.MissError이고, 확인
// 소유가 남은 채 멤버가 바뀌어 확인을 닫은 미스는 dispatchoutbox.ErrHandoffCheckClosed로도 판별된다. 세대 소진은
// sendfence.ErrGenerationExhausted다. 재발급 예산이 없는 failed의 dlq는 commit 뒤 정산 예산과 별도 예산으로 dedup
// claim 키를 해제하고, 해제 실패는 dispatchoutbox.ErrHandoffClaimKeysNotReleased다.
type HandoffConsumer interface {
	dispatchoutbox.HandoffClaimer
	dispatchoutbox.HandoffSettler
}

var _ HandoffConsumer = (*dispatchoutbox.HandoffConsumer)(nil)

// HandoffReconcilerConfig의 Horizon은 alarm_dispatch worker settings handoff_horizon_ms이고 PollInterval은
// alarm_dispatch poll 주기다.
type HandoffReconcilerConfig struct {
	Horizon      time.Duration
	PollInterval time.Duration
}

// HandoffReconciler는 Iris admission을 마친 sent send unit의 handoff 확인 loop다. 정산은 claim 토큰과 요청 ID가
// 그대로일 때만 단위 하나의 단일 문장으로 적용한다.
type HandoffReconciler = handoffloop.Reconciler[dispatchoutbox.HandoffClaim]

// NewHandoffReconciler는 지평과 poll 주기가 1ms 이상이어야 만든다.
func NewHandoffReconciler(consumer HandoffConsumer, client handoffcheck.ReplyStatusClient, config HandoffReconcilerConfig, logger *slog.Logger) (*HandoffReconciler, error) {
	if consumer == nil {
		return nil, errors.New("new alarm dispatch handoff reconciler: consumer is required")
	}

	reconciler, err := handoffloop.New[dispatchoutbox.HandoffClaim](handoffLedger{HandoffConsumer: consumer}, client, handoffloop.Config{
		Name:               "alarm dispatch",
		Horizon:            config.Horizon,
		PollInterval:       config.PollInterval,
		FenceMissMessage:   "alarm dispatch handoff check lost its claim or send unit; not applied",
		CheckClosed:        dispatchoutbox.ErrHandoffCheckClosed,
		CheckClosedMessage: "alarm dispatch handoff check closed without transition; send unit members changed outside the reconciler",
		NotRechecked:       dispatchoutbox.ErrHandoffClaimKeysNotReleased,
		QueryErrorAttrs:    handoffQueryErrorAttrs,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("new alarm dispatch handoff reconciler: %w", err)
	}

	return reconciler, nil
}

func handoffQueryErrorAttrs(err error) []any {
	return []any{slog.String("error_class", dispatchoutbox.ClassifyErrorCode(err))}
}

// handoffLedger는 send unit 확인을 handoffloop에 잇는다. Iris failed 재발급은 발송 실패와 같은 시도 상한과 선형
// backoff를 쓴다.
type handoffLedger struct {
	HandoffConsumer
}

func (handoffLedger) Check(claim *dispatchoutbox.HandoffClaim) handoffloop.Check {
	return handoffloop.Check{
		IrisRequestID:   claim.IrisRequestID,
		Elapsed:         claim.Elapsed,
		Generation:      claim.Generation,
		RetryBudgetLeft: claim.AttemptCount+1 < alarmDispatchMaxAttempts,
		Subject:         slog.Int64("send_unit_id", claim.UnitID),
	}
}

func (l handoffLedger) Complete(ctx context.Context, claim *dispatchoutbox.HandoffClaim) error {
	if err := l.CompleteHandoff(ctx, claim); err != nil {
		return fmt.Errorf("complete handoff: %w", err)
	}

	return nil
}

func (l handoffLedger) Reschedule(ctx context.Context, claim *dispatchoutbox.HandoffClaim, step, horizon time.Duration) error {
	if err := l.RescheduleHandoff(ctx, claim, step, horizon); err != nil {
		return fmt.Errorf("reschedule handoff: %w", err)
	}

	return nil
}

func (l handoffLedger) Reissue(ctx context.Context, claim *dispatchoutbox.HandoffClaim, code, detail string) error {
	if err := l.ReissueAfterHandoff(ctx, claim, alarmDispatchRetryBackoff(claim.AttemptCount+1), code, detail); err != nil {
		return fmt.Errorf("reissue after handoff: %w", err)
	}

	return nil
}

func (l handoffLedger) Exhaust(ctx context.Context, claim *dispatchoutbox.HandoffClaim, code, detail string) error {
	if err := l.DLQAfterHandoff(ctx, claim, code, detail); err != nil {
		return fmt.Errorf("dlq after handoff: %w", err)
	}

	return nil
}

func (l handoffLedger) Quarantine(ctx context.Context, claim *dispatchoutbox.HandoffClaim, code, detail string) error {
	if err := l.QuarantineAfterHandoff(ctx, claim, code, detail); err != nil {
		return fmt.Errorf("quarantine after handoff: %w", err)
	}

	return nil
}
