package handoffloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/reflectutil"
	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-shared/pkg/panicscope"
)

const (
	batchSize         = 32
	softClaim         = 15 * time.Second
	statusTimeout     = 5 * time.Second
	stateTimeout      = 5 * time.Second
	settleConcurrency = 2
)

type Check struct {
	IrisRequestID   string
	Elapsed         time.Duration
	Generation      int
	RetryBudgetLeft bool
	Subject         slog.Attr
}

type Claimer[C any] interface {
	ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]C, error)
	Check(claim *C) Check
}

//nolint:interfacebloat // one settlement method per handoff decision action
type Settler[C any] interface {
	Complete(ctx context.Context, claim *C) error
	Reschedule(ctx context.Context, claim *C, step, horizon time.Duration) error
	Reissue(ctx context.Context, claim *C, code, detail string) error
	Exhaust(ctx context.Context, claim *C, code, detail string) error
	Quarantine(ctx context.Context, claim *C, code, detail string) error
	RoomNotJoined(ctx context.Context, claim *C, code, detail string) error
}

type Ledger[C any] interface {
	Claimer[C]
	Settler[C]
}

type Config struct {
	Name               string
	Horizon            time.Duration
	PollInterval       time.Duration
	FenceMissMessage   string
	ObserveFenceMiss   func(*sendfence.MissError)
	CheckClosed        error
	CheckClosedMessage string
	NotRechecked       error
	QueryErrorAttrs    func(error) []any
}

type Reconciler[C any] struct {
	ledger      Ledger[C]
	client      handoffcheck.ReplyStatusClient
	config      Config
	task        string
	settleSlots chan struct{}
	logger      *slog.Logger
}

func New[C any](ledger Ledger[C], client handoffcheck.ReplyStatusClient, config Config, logger *slog.Logger) (*Reconciler[C], error) {
	if ledger == nil || client == nil || reflectutil.IsNil(client) {
		return nil, errors.New("ledger and reply status client are required")
	}

	if config.Horizon < time.Millisecond || config.PollInterval < time.Millisecond {
		return nil, fmt.Errorf("horizon %s and poll interval %s must be at least 1ms", config.Horizon, config.PollInterval)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Reconciler[C]{
		ledger:      ledger,
		client:      client,
		config:      config,
		task:        strings.ReplaceAll(config.Name, " ", "-") + "-handoff-check",
		settleSlots: make(chan struct{}, settleConcurrency),
		logger:      logger,
	}, nil
}

func (r *Reconciler[C]) Start(ctx context.Context) error {
	for {
		claimed, err := r.RunOnce(ctx)

		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err != nil {
			r.logger.Warn(r.config.Name+" handoff claim failed", slog.Any("error", err))
		}

		if err == nil && claimed == batchSize {
			continue
		}

		if !retry.Sleep(ctx, r.config.PollInterval) {
			return nil
		}
	}
}

func (r *Reconciler[C]) RunOnce(ctx context.Context) (int, error) {
	claimCtx, cancel := context.WithTimeout(ctx, stateTimeout)
	claims, err := r.ledger.ClaimHandoffDue(claimCtx, batchSize, softClaim)

	cancel()

	if err != nil {
		return 0, fmt.Errorf("claim %s handoff checks: %w", r.config.Name, err)
	}

	var wg sync.WaitGroup

	for i := range claims {
		wg.Go(func() {
			panicguard.Run(r.logger, panicscope.BackgroundTask, r.task, func() {
				r.reconcile(ctx, &claims[i])
			})
		})
	}

	wg.Wait()

	return len(claims), nil
}

func (r *Reconciler[C]) reconcile(ctx context.Context, claim *C) {
	check := r.ledger.Check(claim)

	statusCtx, cancel := context.WithTimeout(ctx, statusTimeout)
	snapshot, queryErr := r.client.GetReplyStatus(statusCtx, check.IrisRequestID)

	cancel()

	if ctx.Err() != nil {
		return
	}

	decision := handoffcheck.Decide(handoffcheck.Input{
		RequestID:       check.IrisRequestID,
		Snapshot:        snapshot,
		QueryErr:        queryErr,
		Elapsed:         check.Elapsed,
		Horizon:         r.config.Horizon,
		Generation:      check.Generation,
		RetryBudgetLeft: check.RetryBudgetLeft,
	})

	stateCtx, cancelState := context.WithTimeout(context.WithoutCancel(ctx), stateTimeout)
	defer cancelState()

	err := r.settle(stateCtx, claim, check, decision, queryErr)

	r.logOutcome(check, decision, err)
}

func (r *Reconciler[C]) settle(ctx context.Context, claim *C, check Check, decision handoffcheck.Decision, queryErr error) error {
	select {
	case r.settleSlots <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("wait %s handoff settlement slot: %w", r.config.Name, ctx.Err())
	}

	defer func() { <-r.settleSlots }()

	return r.apply(ctx, claim, check, decision, queryErr)
}

func (r *Reconciler[C]) apply(ctx context.Context, claim *C, check Check, decision handoffcheck.Decision, queryErr error) error {
	var err error

	switch decision.Action {
	case handoffcheck.ActionComplete:
		err = r.ledger.Complete(ctx, claim)
	case handoffcheck.ActionReschedule:
		if queryErr != nil {
			r.logDeferredQuery(check, queryErr)
		}

		err = r.ledger.Reschedule(ctx, claim, handoffcheck.NextStep(check.Elapsed), r.config.Horizon)
	case handoffcheck.ActionReissue:
		err = r.ledger.Reissue(ctx, claim, decision.Code, decision.Detail)
	case handoffcheck.ActionExhausted:
		err = r.ledger.Exhaust(ctx, claim, decision.Code, decision.Detail)
	case handoffcheck.ActionQuarantine:
		err = r.ledger.Quarantine(ctx, claim, decision.Code, decision.Detail)
	case handoffcheck.ActionRoomNotJoined:
		err = r.ledger.RoomNotJoined(ctx, claim, decision.Code, decision.Detail)
	default:
		return fmt.Errorf("apply %s handoff: unsupported action %d", r.config.Name, decision.Action)
	}

	if err != nil {
		return fmt.Errorf("apply %s handoff: %w", r.config.Name, err)
	}

	return nil
}

func (r *Reconciler[C]) logDeferredQuery(check Check, queryErr error) {
	attrs := []any{check.Subject}

	if r.config.QueryErrorAttrs != nil {
		attrs = append(attrs, r.config.QueryErrorAttrs(queryErr)...)
	}

	attrs = append(attrs, slog.String("error", handoffcheck.QueryErrorDetail(queryErr)))

	r.logger.Warn(r.config.Name+" handoff status query deferred", attrs...)
}

func (r *Reconciler[C]) logOutcome(check Check, decision handoffcheck.Decision, err error) {
	attrs := []any{
		check.Subject,
		slog.Int("generation", check.Generation),
		slog.String("observed_action", decision.Action.String()),
		slog.String("reason", decision.Code),
	}

	if err != nil {
		r.logFailure(append(attrs, slog.Any("error", err)), err)

		return
	}

	switch decision.Action {
	case handoffcheck.ActionReissue:
		r.logger.Warn(r.config.Name+" handoff failed; requeued with the next request generation", attrs...)
	case handoffcheck.ActionExhausted:
		r.logger.Error(r.config.Name+" handoff failed; reissue budget exhausted", attrs...)
	case handoffcheck.ActionQuarantine:
		r.logger.Error(r.config.Name+" handoff outcome unknown; quarantined", attrs...)
	case handoffcheck.ActionRoomNotJoined:
		handoffRoomNotJoinedTotal.WithLabelValues(r.config.Name).Inc()
		r.logger.Warn(r.config.Name+" handoff rejected because the bot account is not in the room; delivery closed and room paused", attrs...)
	case handoffcheck.ActionComplete, handoffcheck.ActionReschedule:
	}
}

func (r *Reconciler[C]) logFailure(attrs []any, err error) {
	if errors.Is(err, r.config.CheckClosed) {
		r.logger.Error(r.config.CheckClosedMessage, attrs...)

		return
	}

	if miss, ok := errors.AsType[*sendfence.MissError](err); ok {
		if r.config.ObserveFenceMiss != nil {
			r.config.ObserveFenceMiss(miss)
		}

		r.logger.Warn(r.config.FenceMissMessage, attrs...)

		return
	}

	if errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, r.config.NotRechecked) {
		r.logger.Warn(r.config.Name+" handoff settlement exceeded its budget; the next check decides again", attrs...)

		return
	}

	r.logger.Error(r.config.Name+" handoff check failed", attrs...)
}
