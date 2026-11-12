// Package handoffloop는 발송 원장(alarm dispatch·youtube delivery)의 Iris handoff 확인 실행
// 루프다. 확인 claim, 트랜잭션 밖 상태 조회, 정산 슬롯과 예산, 정산 결과의 로그 수준을 한 곳에서 정한다. 판정 규칙과
// 확인 간격은 handoffcheck가, 원장 상태 어휘·전이 SQL·시도 예산·원장 지표는 각 원장 어댑터가 소유한다.
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

// 시간 예산: claim은 확인의 다음 시각을 softClaim 뒤로 미루고, 확인마다 상태 조회(최대 statusTimeout)와 정산(최대
// stateTimeout, 정산 슬롯 대기·연결 획득·문장이나 트랜잭션 포함)을 차례로 끝낸다. 조회와 정산 예산의 합 10s가 soft
// claim보다 짧으므로 같은 확인을 다른 확인이 고르기 전에 이 확인의 적용이 끝나거나 포기된다. 예산을 넘긴 정산은
// 적용하지 않고, soft claim 뒤 다음 확인이 토큰 CAS로 다시 판정한다.
const (
	// 한 batch의 상태 조회는 모두 병렬로 돈다. 동시 외부 조회 수의 상한이다.
	batchSize     = 32
	softClaim     = 15 * time.Second
	statusTimeout = 5 * time.Second
	// 확인 claim과 정산 하나의 DB 예산이다.
	stateTimeout = 5 * time.Second
	// 정산은 일반 연결 획득으로 확인 하나씩이다. 동시에 쓰는 연결을 이 수로 묶어 batch 정산이 alarm_worker 풀의 일반
	// 용량을 독점하지 않게 한다.
	settleConcurrency = 2
)

// Check는 원장 claim 하나에서 엔진이 판정과 로그에 쓰는 값이다.
type Check struct {
	// IrisRequestID는 admission 202에서 저장한 Iris requestId다.
	IrisRequestID string
	// Elapsed는 원장 DB 시계로 잰 admission 뒤 경과다.
	Elapsed    time.Duration
	Generation int
	// RetryBudgetLeft는 원장 시도 예산으로 한 번 더 보낼 수 있는지다.
	RetryBudgetLeft bool
	// Subject는 확인 대상을 가리키는 로그 속성이다. Iris requestId는 넣지 않는다.
	Subject slog.Attr
}

// Claimer는 확인 시각이 된 원장 확인을 soft claim하고, claim 하나의 판정 입력을 읽는다. C는 원장 claim 타입이다.
type Claimer[C any] interface {
	ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]C, error)
	Check(claim *C) Check
}

// Settler는 handoffcheck 행동별 원장 정산이다. 정산은 claim 토큰과 요청이 그대로인 확인 하나에만 적용하고, 아무것도
// 바꾸지 않은 fence 미스는 *sendfence.MissError로 돌려준다.
type Settler[C any] interface {
	Complete(ctx context.Context, claim *C) error
	Reschedule(ctx context.Context, claim *C, step, horizon time.Duration) error
	Reissue(ctx context.Context, claim *C, code, detail string) error
	Exhaust(ctx context.Context, claim *C, code, detail string) error
	Quarantine(ctx context.Context, claim *C, code, detail string) error
}

// Ledger는 원장 하나의 handoff 확인 어댑터다.
type Ledger[C any] interface {
	Claimer[C]
	Settler[C]
}

// Config는 원장별 지평·주기와 정산 실패 분류다. 원장별 로그 문구는 runbook이 인용하므로 "<Name> handoff …" 형식과
// 아래 원장별 문구로 원장마다 그대로 남긴다.
type Config struct {
	// Name은 로그·오류 문구와 panic 작업 이름에 쓰는 원장 이름이다(예: "alarm dispatch").
	Name string
	// Horizon은 원장 worker settings handoff_horizon_ms이고, PollInterval은 batch가 가득 차지 않았을 때 쉬는 주기다.
	Horizon      time.Duration
	PollInterval time.Duration
	// FenceMissMessage는 확인을 닫지 않은 fence 미스의 경고 문구이고, ObserveFenceMiss는 그 미스를 원장 지표에 남긴다.
	FenceMissMessage string
	ObserveFenceMiss func(*sendfence.MissError)
	// CheckClosed는 확인 소유가 남은 채 확인을 영구히 닫은 정산 실패의 sentinel이고, CheckClosedMessage는 그 오류
	// 문구다. 이런 종료가 없는 원장은 둘 다 비운다.
	CheckClosed        error
	CheckClosedMessage string
	// NotRechecked는 확인이 닫힌 뒤의 후속 작업 실패라 deadline이 섞여도 다음 확인이 다시 판정하지 않는 sentinel이다.
	NotRechecked error
	// QueryErrorAttrs는 다음 확인으로 미룬 상태 조회 오류 경고에 더할 원장별 속성이다.
	QueryErrorAttrs func(error) []any
}

// Reconciler는 원장 하나의 Iris handoff 확인 loop다. 확인 시각이 된 확인의 /reply-status를 트랜잭션 밖에서 조회하고,
// 그 관측의 판정을 원장 정산으로 적용한다.
type Reconciler[C any] struct {
	ledger      Ledger[C]
	client      handoffcheck.ReplyStatusClient
	config      Config
	task        string
	settleSlots chan struct{}
	logger      *slog.Logger
}

// New는 지평과 poll 주기가 1ms 이상이어야 만든다. 로거가 없으면 slog.Default()를 쓴다.
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

// Start는 ctx가 끝날 때만 반환한다. 가득 찬 batch 뒤에는 바로 다음 batch를 고르고, 그 밖에는 poll 주기만큼 쉰다.
// 확인 실패는 Start 오류가 아니다. 첫 오류가 나면 runNamedSchedulers가 형제 egress runner를 모두 취소한다.
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

// RunOnce는 확인 시각이 된 확인을 한 batch 고르고, 확인마다 상태 조회와 정산을 병렬로 끝낸 뒤 고른 수를 돌려준다.
// 확인 claim은 batch 조회라 일반 연결 획득을 쓴다.
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

	// 종료 중 끊긴 조회는 관측이 아니다. soft claim이 끝나면 다음 확인이 다시 판정한다.
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

	// 관측을 얻은 뒤의 정산은 종료 취소와 분리하되, 정산 슬롯 대기까지 포함한 예산 안에서만 한다.
	stateCtx, cancelState := context.WithTimeout(context.WithoutCancel(ctx), stateTimeout)
	defer cancelState()

	err := r.settle(stateCtx, claim, check, decision, queryErr)

	r.logOutcome(check, decision, err)
}

// settle은 정산 슬롯을 얻어 관측을 적용한다. 예산 안에 슬롯을 얻지 못하면 적용하지 않는다.
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
	default:
		return fmt.Errorf("apply %s handoff: unsupported action %d", r.config.Name, decision.Action)
	}

	if err != nil {
		return fmt.Errorf("apply %s handoff: %w", r.config.Name, err)
	}

	return nil
}

// logDeferredQuery는 다음 확인으로 미룬 조회 오류를 남긴다. SDK 오류 원문은 /reply-status/{requestId} 경로를 담으므로
// HTTP 상태만 남긴다.
func (r *Reconciler[C]) logDeferredQuery(check Check, queryErr error) {
	attrs := []any{check.Subject}

	if r.config.QueryErrorAttrs != nil {
		attrs = append(attrs, r.config.QueryErrorAttrs(queryErr)...)
	}

	attrs = append(attrs, slog.String("error", handoffcheck.QueryErrorDetail(queryErr)))

	r.logger.Warn(r.config.Name+" handoff status query deferred", attrs...)
}

// logOutcome은 정산 결과를 남긴다. 재발급은 경고, 소진과 격리는 오류다.
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
	case handoffcheck.ActionComplete, handoffcheck.ActionReschedule:
	}
}

// logFailure는 정산 실패를 다음 확인이 이어받는지로 나눈다. 확인을 영구히 닫은 실패는 관측 결과가 원장에 반영되지
// 않은 채 다시 확인하지 않으므로 오류다. 확인을 닫지 않은 fence 미스는 다음 확인이나 다른 정산이 이어받으므로
// 경고다. 예산을 넘긴 정산은 soft claim 뒤 다음 확인이 다시 판정하므로 경고지만, NotRechecked는 deadline이어도
// 다시 시도되지 않으므로 오류다.
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
