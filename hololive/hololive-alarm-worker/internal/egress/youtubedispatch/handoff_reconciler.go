package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/reflectutil"
	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

// 시간 예산: claim은 요청의 다음 확인을 soft claim(15s) 뒤로 미루고, 요청마다 상태 조회(최대 5s)와 정산(최대
// deliveryStateTimeout 5s, 정산 슬롯 대기·연결 획득·부모 outbox 집계를 포함한 트랜잭션)을 차례로 끝낸다. 조회와 정산
// 예산의 합 10s가 soft claim보다 짧으므로 같은 요청을 다른 확인이 고르기 전에 이 확인의 적용이 끝나거나 포기된다.
// 예산을 넘긴 정산은 적용하지 않고, soft claim 뒤 다음 확인이 토큰 CAS로 다시 판정한다.
const (
	// 한 batch의 상태 조회는 모두 병렬로 돈다. 동시 외부 조회 수의 상한이다.
	youtubeHandoffBatchSize     = 32
	youtubeHandoffSoftClaim     = 15 * time.Second
	youtubeHandoffStatusTimeout = 5 * time.Second
	// 정산은 일반 연결 획득으로 요청 하나의 트랜잭션 하나씩이다. 동시에 쓰는 연결을 이 수로 묶어 batch 정산이
	// alarm_worker 풀의 일반 용량을 독점하지 않게 한다.
	youtubeHandoffSettleConcurrency = 2
)

// handoffStore는 youtube 고정 요청 handoff 확인의 claim과 정산이다.
type handoffStore interface {
	handoffClaimer
	handoffSettler
}

// handoffClaimer는 확인 시각이 된 확인 대기 고정 요청을 soft claim한다.
type handoffClaimer interface {
	ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]store.HandoffClaim, error)
}

// handoffSettler는 claim한 고정 요청의 정산이다. 정산은 요청 하나의 단일 문장 또는 트랜잭션이고, 소유 상실과 정산 중
// 논리 그룹의 부모 집합 확장은 아무것도 바꾸지 않은 *sendfence.MissError, 멤버 변경 종료는 store.ErrHandoffCheckClosed다.
// 재발급·소진·격리는 바뀐 delivery의 부모 outbox 집계까지 같은 트랜잭션에서 기록하므로 정산 뒤 따로 집계하지 않는다.
type handoffSettler interface {
	CompleteHandoff(ctx context.Context, claim *store.HandoffClaim) (store.ApplyResult, error)
	RescheduleHandoff(ctx context.Context, claim *store.HandoffClaim, step, horizon time.Duration) (store.ApplyResult, error)
	ReissueAfterHandoff(ctx context.Context, claim *store.HandoffClaim, code, detail string) (store.ApplyResult, error)
	ExhaustAfterHandoff(ctx context.Context, claim *store.HandoffClaim, code, detail string) (store.ApplyResult, error)
	QuarantineAfterHandoff(ctx context.Context, claim *store.HandoffClaim, code, detail string) (store.ApplyResult, error)
}

// handoffReconcilerConfig의 Horizon은 youtube_delivery executor attempt_timeout(DeliverySendTimeout), PollInterval은
// youtube_delivery poll 주기, MaxRetries는 발송 실패 정책과 같은 시도 예산이다.
type handoffReconcilerConfig struct {
	Horizon      time.Duration
	PollInterval time.Duration
	MaxRetries   int
}

// HandoffReconciler는 Iris admission을 마친 youtube 고정 요청의 /reply-status를 확인 시각마다 조회해 정산한다. 외부
// 조회는 트랜잭션 밖에서 하고, 정산은 claim 토큰·requestId·요청 세대·본문 해시·고정 멤버와 멤버 행 버전이 그대로일
// 때만 요청 하나의 트랜잭션으로 적용한다. 판정 규칙과 확인 간격은 notification·alarm dispatch와 같은 handoffcheck다.
type HandoffReconciler struct {
	store        handoffStore
	client       handoffcheck.ReplyStatusClient
	horizon      time.Duration
	pollInterval time.Duration
	maxRetries   int
	settleSlots  chan struct{}
	logger       *slog.Logger
}

// NewHandoffReconciler는 youtube_delivery dispatcher와 같은 설정으로 전이 저장소를 만든다. 지평은 DeliverySendTimeout이다.
func NewHandoffReconciler(
	db deliverysql.DeliveryDB,
	client handoffcheck.ReplyStatusClient,
	logger *slog.Logger,
	config *Config,
) (*HandoffReconciler, error) {
	if db == nil || reflectutil.IsNil(db) {
		return nil, errors.New("new youtube handoff reconciler: db is required")
	}

	if config == nil {
		return nil, errors.New("new youtube handoff reconciler: config is required")
	}

	validConfig := *config
	if err := validConfig.Validate(); err != nil {
		return nil, fmt.Errorf("new youtube handoff reconciler: %w", err)
	}

	initOutboxMetrics()

	logger = dispatcherLogger(logger)

	transitionStore, err := newDispatcherTransitionStore(db, logger, validConfig)
	if err != nil {
		return nil, fmt.Errorf("new youtube handoff reconciler: %w", err)
	}

	return newHandoffReconciler(
		transitionStore,
		client,
		handoffReconcilerConfig{
			Horizon: validConfig.DeliverySendTimeout, PollInterval: validConfig.PollInterval, MaxRetries: validConfig.MaxRetries,
		},
		logger,
	)
}

func newHandoffReconciler(
	handoffs handoffStore,
	client handoffcheck.ReplyStatusClient,
	config handoffReconcilerConfig,
	logger *slog.Logger,
) (*HandoffReconciler, error) {
	if handoffs == nil || client == nil || reflectutil.IsNil(client) {
		return nil, errors.New("new youtube handoff reconciler: store and reply status client are required")
	}

	if config.Horizon < time.Millisecond || config.PollInterval < time.Millisecond {
		return nil, fmt.Errorf("new youtube handoff reconciler: horizon %s and poll interval %s must be at least 1ms",
			config.Horizon, config.PollInterval)
	}

	if config.MaxRetries <= 0 {
		return nil, fmt.Errorf("new youtube handoff reconciler: max retries %d must be positive", config.MaxRetries)
	}

	return &HandoffReconciler{
		store:        handoffs,
		client:       client,
		horizon:      config.Horizon,
		pollInterval: config.PollInterval,
		maxRetries:   config.MaxRetries,
		settleSlots:  make(chan struct{}, youtubeHandoffSettleConcurrency),
		logger:       dispatcherLogger(logger),
	}, nil
}

// Start는 ctx가 끝날 때만 반환한다. 가득 찬 batch 뒤에는 바로 다음 batch를 고르고, 그 밖에는 poll 주기만큼 쉰다.
// 확인 실패는 Start 오류가 아니다. 첫 오류가 나면 runNamedSchedulers가 형제 egress runner를 모두 취소한다.
func (r *HandoffReconciler) Start(ctx context.Context) error {
	for {
		claimed, err := r.RunOnce(ctx)

		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err != nil {
			r.logger.Warn("youtube delivery handoff claim failed", slog.Any("error", err))
		}

		if err == nil && claimed == youtubeHandoffBatchSize {
			continue
		}

		if !retry.Sleep(ctx, r.pollInterval) {
			return nil
		}
	}
}

// RunOnce는 확인 시각이 된 고정 요청을 한 batch 고르고, 요청마다 상태 조회와 정산을 병렬로 끝낸 뒤 고른 수를
// 돌려준다. 확인 claim은 batch 조회라 일반 연결 획득을 쓴다.
func (r *HandoffReconciler) RunOnce(ctx context.Context) (int, error) {
	claimCtx, cancel := context.WithTimeout(ctx, deliveryStateTimeout)
	claims, err := r.store.ClaimHandoffDue(claimCtx, youtubeHandoffBatchSize, youtubeHandoffSoftClaim)

	cancel()

	if err != nil {
		return 0, fmt.Errorf("claim youtube delivery handoff checks: %w", err)
	}

	var wg sync.WaitGroup

	for i := range claims {
		wg.Go(func() {
			panicguard.Run(r.logger, panicscope.BackgroundTask, "youtube-delivery-handoff-check", func() {
				r.reconcile(ctx, &claims[i])
			})
		})
	}

	wg.Wait()

	return len(claims), nil
}

func (r *HandoffReconciler) reconcile(ctx context.Context, claim *store.HandoffClaim) {
	statusCtx, cancel := context.WithTimeout(ctx, youtubeHandoffStatusTimeout)
	snapshot, queryErr := r.client.GetReplyStatus(statusCtx, claim.IrisRequestID)

	cancel()

	// 종료 중 끊긴 조회는 관측이 아니다. soft claim이 끝나면 다음 확인이 다시 판정한다.
	if ctx.Err() != nil {
		return
	}

	decision := handoffcheck.Decide(handoffcheck.Input{
		RequestID:       claim.IrisRequestID,
		Snapshot:        snapshot,
		QueryErr:        queryErr,
		Elapsed:         claim.Elapsed,
		Horizon:         r.horizon,
		Generation:      claim.Generation,
		RetryBudgetLeft: claim.AttemptCount+1 < r.maxRetries,
	})

	// 관측을 얻은 뒤의 정산은 종료 취소와 분리하되, 정산 슬롯 대기까지 포함한 예산 안에서만 한다. 연결은 일반
	// 획득이다(control 예약분을 쓰지 않는다).
	stateCtx, cancelState := context.WithTimeout(context.WithoutCancel(ctx), deliveryStateTimeout)
	defer cancelState()

	err := r.settle(stateCtx, claim, decision, queryErr)

	r.logOutcome(claim, decision, err)
}

// settle은 정산 슬롯을 얻어 관측을 적용한다. 예산 안에 슬롯을 얻지 못하면 적용하지 않는다.
func (r *HandoffReconciler) settle(ctx context.Context, claim *store.HandoffClaim, decision handoffcheck.Decision, queryErr error) error {
	select {
	case r.settleSlots <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("wait youtube delivery handoff settlement slot: %w", ctx.Err())
	}

	defer func() { <-r.settleSlots }()

	return r.apply(ctx, claim, decision, queryErr)
}

func (r *HandoffReconciler) apply(ctx context.Context, claim *store.HandoffClaim, decision handoffcheck.Decision, queryErr error) error {
	var (
		result store.ApplyResult
		err    error
	)

	switch decision.Action {
	case handoffcheck.ActionComplete:
		result, err = r.store.CompleteHandoff(ctx, claim)
	case handoffcheck.ActionReschedule:
		if queryErr != nil {
			// SDK 오류 원문은 /reply-status/{requestId} 경로를 담으므로 HTTP 상태만 남긴다.
			r.logger.Warn("youtube delivery handoff status query deferred",
				slog.Any("delivery_ids", claim.MemberIDs),
				slog.String("error", handoffcheck.QueryErrorDetail(queryErr)))
		}

		result, err = r.store.RescheduleHandoff(ctx, claim, handoffcheck.NextStep(claim.Elapsed), r.horizon)
	case handoffcheck.ActionReissue:
		result, err = r.store.ReissueAfterHandoff(ctx, claim, decision.Code, decision.Detail)
	case handoffcheck.ActionExhausted:
		result, err = r.store.ExhaustAfterHandoff(ctx, claim, decision.Code, decision.Detail)
	case handoffcheck.ActionQuarantine:
		result, err = r.store.QuarantineAfterHandoff(ctx, claim, decision.Code, decision.Detail)
	default:
		return fmt.Errorf("apply youtube delivery handoff: unsupported action %d", decision.Action)
	}

	observeLifecycleApply("handoff_"+decision.Action.String(), result, err, len(claim.MemberIDs))

	if err != nil {
		return fmt.Errorf("apply youtube delivery handoff: %w", err)
	}

	if result.Outcome != store.ApplyApplied {
		return fmt.Errorf("apply youtube delivery handoff: outcome %s", result.Outcome)
	}

	r.observeLedger(decision.Action, result, len(claim.MemberIDs))

	return nil
}

// observeLedger는 실패 정산이 지운 SENT 원장과 격리가 기록한 QUARANTINED 원장 수를 남긴다. 적용된 정산은 고정 멤버
// 하나당 논리 키 하나를 바꾼다.
func (r *HandoffReconciler) observeLedger(action handoffcheck.Action, result store.ApplyResult, units int) {
	switch action {
	case handoffcheck.ActionReissue, handoffcheck.ActionExhausted:
		observeLedgerOperation("handoff_delete_sent", result, units)
	case handoffcheck.ActionQuarantine:
		observeLedgerOperation("handoff_delete_sent", result, units)
		observeLedgerOperation("record_quarantined", result, units)
	case handoffcheck.ActionComplete, handoffcheck.ActionReschedule:
	}
}

// logOutcome은 정산 결과를 남긴다. 로그에는 고정 멤버 delivery id와 요청 세대만 남기고 Iris requestId는 남기지 않는다.
// 확인을 닫지 않은 fence 미스(소유 상실, 정산 중 논리 그룹에 새 follower·outbox가 들어옴)는 다른 정산이나 soft claim 뒤
// 다음 확인이 이어받으므로 경고다. 멤버 변경으로 확인을 닫은 종료는 다음 확인이 없으므로 오류다. 예산을 넘긴 정산은
// soft claim 뒤 다음 확인이 토큰 CAS로 다시 판정하므로 경고다.
func (r *HandoffReconciler) logOutcome(claim *store.HandoffClaim, decision handoffcheck.Decision, err error) {
	attrs := []any{
		slog.Any("delivery_ids", claim.MemberIDs),
		slog.Int("generation", claim.Generation),
		slog.String("observed_action", decision.Action.String()),
		slog.String("reason", decision.Code),
	}

	if err != nil {
		switch {
		case errors.Is(err, store.ErrHandoffCheckClosed):
			r.logger.Error("youtube delivery handoff check closed; frozen members changed since admission",
				append(attrs, slog.Any("error", err))...)
		case isFenceMiss(err):
			r.logger.Warn("youtube delivery handoff check lost its claim or logical group changed; not applied",
				append(attrs, slog.Any("error", err))...)
		case errors.Is(err, context.DeadlineExceeded):
			r.logger.Warn("youtube delivery handoff settlement exceeded its budget; the next check decides again",
				append(attrs, slog.Any("error", err))...)
		default:
			r.logger.Error("youtube delivery handoff check failed", append(attrs, slog.Any("error", err))...)
		}

		return
	}

	switch decision.Action {
	case handoffcheck.ActionReissue:
		r.logger.Warn("youtube delivery handoff failed; requeued with the next request generation", attrs...)
	case handoffcheck.ActionExhausted:
		r.logger.Error("youtube delivery handoff failed; reissue budget exhausted", attrs...)
	case handoffcheck.ActionQuarantine:
		r.logger.Error("youtube delivery handoff outcome unknown; quarantined", attrs...)
	case handoffcheck.ActionComplete, handoffcheck.ActionReschedule:
	}
}

func isFenceMiss(err error) bool {
	_, ok := errors.AsType[*sendfence.MissError](err)

	return ok
}
