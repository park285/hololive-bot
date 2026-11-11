package notificationdelivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffloop"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
)

// HandoffStore는 notification handoff 확인의 claim과 적용이다.
type HandoffStore interface {
	HandoffClaimer
	HandoffSettler
}

// HandoffClaimer는 확인 시각이 된 확인 대기 행을 soft claim한다.
type HandoffClaimer interface {
	ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error)
}

// HandoffSettler는 handoff 관측을 적용한다. 적용은 행 하나의 단일 문장이고, fence 미스는 행을 바꾸지 않은
// *sendfence.MissError다. 세대 소진은 sendfence.ErrGenerationExhausted다.
type HandoffSettler interface {
	CompleteHandoff(ctx context.Context, claim *HandoffClaim) error
	RescheduleHandoff(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) error
	ReissueAfterHandoff(ctx context.Context, claim *HandoffClaim, retryAfter time.Duration, code, detail string) error
	ExhaustAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error
	QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error
}

var _ HandoffStore = (*Store)(nil)

// HandoffReconcilerConfig의 Horizon은 notification_delivery profile handoff_horizon_ms, PollInterval은
// notification_delivery poll 주기다. MaxRetries·RetryBackoff는 발송 실패 정책과 같은 profile 값이며 failed 재발급의
// 시도 예산과 다음 시도 지연이다.
type HandoffReconcilerConfig struct {
	Horizon      time.Duration
	PollInterval time.Duration
	MaxRetries   int
	RetryBackoff time.Duration
}

// HandoffReconciler는 Iris admission을 마친 SENT 행의 handoff 확인 loop다. 정산은 claim 토큰·요청 ID·요청 세대가
// 그대로일 때만 행 하나의 단일 문장으로 적용한다.
type HandoffReconciler = handoffloop.Reconciler[HandoffClaim]

// NewHandoffReconciler는 지평과 poll 주기가 1ms 이상이고 시도 예산·재시도 지연이 양수여야 만든다.
func NewHandoffReconciler(store HandoffStore, client handoffcheck.ReplyStatusClient, config HandoffReconcilerConfig, logger *slog.Logger) (*HandoffReconciler, error) {
	if store == nil {
		return nil, errors.New("new notification handoff reconciler: store is required")
	}

	if config.MaxRetries <= 0 || config.RetryBackoff <= 0 {
		return nil, fmt.Errorf("new notification handoff reconciler: max retries %d and retry backoff %s must be positive", config.MaxRetries, config.RetryBackoff)
	}

	ledger := handoffLedger{HandoffStore: store, maxRetries: config.MaxRetries, retryBackoff: config.RetryBackoff}

	// 이 원장은 다음 확인 시각이 확인 대기 SENT 행에만 있어 토큰이 남은 채 확인만 닫히는 미스가 없다.
	reconciler, err := handoffloop.New[HandoffClaim](ledger, client, handoffloop.Config{
		Name:             "notification delivery",
		Horizon:          config.Horizon,
		PollInterval:     config.PollInterval,
		FenceMissMessage: "notification delivery handoff check lost its claim; not applied",
		ObserveFenceMiss: observeHandoffFenceMiss,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("new notification handoff reconciler: %w", err)
	}

	return reconciler, nil
}

// observeHandoffFenceMiss는 토큰이 바뀌어 적용하지 못한 handoff 정산을 행별 fence miss 지표에 남긴다.
func observeHandoffFenceMiss(miss *sendfence.MissError) {
	for _, row := range miss.Rows {
		notificationDeliveryFenceMissTotal.WithLabelValues(miss.Action, string(row.Reason), fenceMissResultFenceLost).Inc()
	}
}

// handoffLedger는 확인 대기 SENT 행을 handoffloop에 잇는다. Iris failed 재발급은 발송 실패와 같은 profile 시도 예산과
// 재시도 지연을 쓴다.
type handoffLedger struct {
	HandoffStore

	maxRetries   int
	retryBackoff time.Duration
}

func (l handoffLedger) Check(claim *HandoffClaim) handoffloop.Check {
	return handoffloop.Check{
		IrisRequestID:   claim.IrisRequestID,
		Elapsed:         claim.Elapsed,
		Generation:      claim.Generation,
		RetryBudgetLeft: retryBudgetLeft(claim.AttemptCount, l.maxRetries),
		Subject:         slog.Int64("id", claim.ID),
	}
}

func (l handoffLedger) Complete(ctx context.Context, claim *HandoffClaim) error {
	if err := l.CompleteHandoff(ctx, claim); err != nil {
		return fmt.Errorf("complete handoff: %w", err)
	}

	return nil
}

func (l handoffLedger) Reschedule(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) error {
	if err := l.RescheduleHandoff(ctx, claim, step, horizon); err != nil {
		return fmt.Errorf("reschedule handoff: %w", err)
	}

	return nil
}

func (l handoffLedger) Reissue(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	if err := l.ReissueAfterHandoff(ctx, claim, l.retryBackoff, code, detail); err != nil {
		return fmt.Errorf("reissue after handoff: %w", err)
	}

	return nil
}

func (l handoffLedger) Exhaust(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	if err := l.ExhaustAfterHandoff(ctx, claim, code, detail); err != nil {
		return fmt.Errorf("exhaust after handoff: %w", err)
	}

	return nil
}

func (l handoffLedger) Quarantine(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	if err := l.QuarantineAfterHandoff(ctx, claim, code, detail); err != nil {
		return fmt.Errorf("quarantine after handoff: %w", err)
	}

	return nil
}
