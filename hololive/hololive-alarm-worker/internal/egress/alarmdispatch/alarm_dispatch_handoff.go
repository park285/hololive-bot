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

type HandoffConsumer interface {
	dispatchoutbox.HandoffClaimer
	dispatchoutbox.HandoffSettler
}

var _ HandoffConsumer = (*dispatchoutbox.HandoffConsumer)(nil)

type HandoffReconcilerConfig struct {
	Horizon      time.Duration
	PollInterval time.Duration
}

func NewHandoffReconciler(consumer HandoffConsumer, client handoffcheck.ReplyStatusClient, config HandoffReconcilerConfig, logger *slog.Logger) (*handoffloop.Reconciler[dispatchoutbox.HandoffClaim], error) {
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
