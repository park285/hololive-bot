package notificationdelivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/retry"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
)

// 시간 예산: claim은 행의 다음 확인을 soft claim(15s) 뒤로 미루고, 행마다 상태 조회(최대 5s)와 정산(최대
// NotificationDeliveryFinalizeTimeout 5s, 정산 슬롯 대기·연결 획득·문장 포함)을 차례로 끝낸다. 조회와 정산 예산의 합
// 10s가 soft claim보다 짧으므로 같은 행을 다른 확인이 고르기 전에 이 확인의 적용이 끝나거나 포기된다. 예산을 넘긴
// 정산은 적용하지 않고, soft claim 뒤 다음 확인이 토큰 CAS로 다시 판정한다.
const (
	// 한 batch의 상태 조회는 모두 병렬로 돈다. 동시 외부 조회 수의 상한이다.
	notificationHandoffBatchSize     = 32
	notificationHandoffSoftClaim     = 15 * time.Second
	notificationHandoffStatusTimeout = 5 * time.Second
	// 정산은 일반 연결 획득으로 단일 문장 하나씩이다. 동시에 쓰는 연결을 이 수로 묶어 batch 정산이 alarm_worker 풀의
	// 일반 용량을 독점하지 않게 한다. 슬롯 대기까지 포함해 정산 예산을 넘긴 정산은 적용하지 않고, soft claim 뒤 다음
	// 확인이 다시 판정한다.
	notificationHandoffSettleConcurrency = 2
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

// ReplyStatusClient는 Iris /reply-status 조회다.
type ReplyStatusClient interface {
	GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error)
}

// HandoffReconcilerConfig의 Horizon은 notification_delivery executor attempt timeout, PollInterval은
// notification_delivery poll 주기다. MaxRetries·RetryBackoff는 발송 실패 정책과 같은 profile 값이며 failed 재발급의
// 시도 예산과 다음 시도 지연이다.
type HandoffReconcilerConfig struct {
	Horizon      time.Duration
	PollInterval time.Duration
	MaxRetries   int
	RetryBackoff time.Duration
}

// HandoffReconciler는 Iris admission을 마친 SENT 행의 /reply-status를 확인 시각마다 조회해 정산한다. 외부 조회는
// 트랜잭션 밖에서 하고, 정산은 claim 토큰·요청 ID·요청 세대가 그대로일 때만 행 하나의 단일 문장으로 적용한다.
type HandoffReconciler struct {
	store        HandoffStore
	client       ReplyStatusClient
	horizon      time.Duration
	pollInterval time.Duration
	maxRetries   int
	retryBackoff time.Duration
	settleSlots  chan struct{}
	logger       *slog.Logger
}

// NewHandoffReconciler는 지평과 poll 주기가 1ms 이상이고 시도 예산·재시도 지연이 양수여야 만든다.
func NewHandoffReconciler(store HandoffStore, client ReplyStatusClient, config HandoffReconcilerConfig, logger *slog.Logger) (*HandoffReconciler, error) {
	if store == nil || client == nil {
		return nil, errors.New("new notification handoff reconciler: store and reply status client are required")
	}

	if config.Horizon < time.Millisecond || config.PollInterval < time.Millisecond {
		return nil, fmt.Errorf("new notification handoff reconciler: horizon %s and poll interval %s must be at least 1ms", config.Horizon, config.PollInterval)
	}

	if config.MaxRetries <= 0 || config.RetryBackoff <= 0 {
		return nil, fmt.Errorf("new notification handoff reconciler: max retries %d and retry backoff %s must be positive", config.MaxRetries, config.RetryBackoff)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &HandoffReconciler{
		store:        store,
		client:       client,
		horizon:      config.Horizon,
		pollInterval: config.PollInterval,
		maxRetries:   config.MaxRetries,
		retryBackoff: config.RetryBackoff,
		settleSlots:  make(chan struct{}, notificationHandoffSettleConcurrency),
		logger:       logger,
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
			r.logger.Warn("notification delivery handoff claim failed", slog.Any("error", err))
		}

		if err == nil && claimed == notificationHandoffBatchSize {
			continue
		}

		if !retry.Sleep(ctx, r.pollInterval) {
			return nil
		}
	}
}

// RunOnce는 확인 시각이 된 행을 한 batch 고르고, 행마다 상태 조회와 정산을 병렬로 끝낸 뒤 고른 수를 돌려준다. 확인
// claim은 batch 조회라 일반 연결 획득을 쓴다.
func (r *HandoffReconciler) RunOnce(ctx context.Context) (int, error) {
	claimCtx, cancel := context.WithTimeout(ctx, workerconfig.NotificationDeliveryFinalizeTimeout)
	claims, err := r.store.ClaimHandoffDue(claimCtx, notificationHandoffBatchSize, notificationHandoffSoftClaim)

	cancel()

	if err != nil {
		return 0, fmt.Errorf("claim notification delivery handoff checks: %w", err)
	}

	var wg sync.WaitGroup

	for i := range claims {
		wg.Go(func() {
			panicguard.Run(r.logger, panicguard.BackgroundTask, "notification-delivery-handoff-check", func() {
				r.reconcile(ctx, &claims[i])
			})
		})
	}

	wg.Wait()

	return len(claims), nil
}

func (r *HandoffReconciler) reconcile(ctx context.Context, claim *HandoffClaim) {
	statusCtx, cancel := context.WithTimeout(ctx, notificationHandoffStatusTimeout)
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
		RetryBudgetLeft: retryBudgetLeft(claim.AttemptCount, r.maxRetries),
	})

	// 관측을 얻은 뒤의 정산은 종료 취소와 분리하되, 정산 슬롯 대기까지 포함한 예산 안에서만 한다.
	stateCtx, cancelState := context.WithTimeout(context.WithoutCancel(ctx), workerconfig.NotificationDeliveryFinalizeTimeout)
	defer cancelState()

	err := r.settle(stateCtx, claim, decision, queryErr)

	r.logOutcome(claim, decision, err)
}

// settle은 정산 슬롯을 얻어 관측을 적용한다. 예산 안에 슬롯을 얻지 못하면 적용하지 않는다.
func (r *HandoffReconciler) settle(ctx context.Context, claim *HandoffClaim, decision handoffcheck.Decision, queryErr error) error {
	select {
	case r.settleSlots <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("wait notification delivery handoff settlement slot: %w", ctx.Err())
	}

	defer func() { <-r.settleSlots }()

	return r.apply(ctx, claim, decision, queryErr)
}

func (r *HandoffReconciler) apply(ctx context.Context, claim *HandoffClaim, decision handoffcheck.Decision, queryErr error) error {
	var err error

	switch decision.Action {
	case handoffcheck.ActionComplete:
		err = r.store.CompleteHandoff(ctx, claim)
	case handoffcheck.ActionReschedule:
		if queryErr != nil {
			// SDK 오류 원문은 /reply-status/{requestId} 경로를 담으므로 HTTP 상태만 남긴다.
			r.logger.Warn("notification delivery handoff status query deferred",
				slog.Int64("id", claim.ID),
				slog.String("error", handoffcheck.QueryErrorDetail(queryErr)))
		}

		err = r.store.RescheduleHandoff(ctx, claim, handoffcheck.NextStep(claim.Elapsed), r.horizon)
	case handoffcheck.ActionReissue:
		err = r.store.ReissueAfterHandoff(ctx, claim, r.retryBackoff, decision.Code, decision.Detail)
	case handoffcheck.ActionExhausted:
		err = r.store.ExhaustAfterHandoff(ctx, claim, decision.Code, decision.Detail)
	case handoffcheck.ActionQuarantine:
		err = r.store.QuarantineAfterHandoff(ctx, claim, decision.Code, decision.Detail)
	default:
		return fmt.Errorf("apply notification delivery handoff: unsupported action %d", decision.Action)
	}

	if err != nil {
		return fmt.Errorf("apply notification delivery handoff: %w", err)
	}

	return nil
}

// logOutcome은 정산 결과를 남긴다. 적용 fence 미스는 토큰이 바뀐 소유 상실이라 다음 확인이나 다른 정산이 이어받으므로
// 지표와 경고다. 이 원장은 다음 확인 시각이 확인 대기 SENT 행에만 있어 토큰이 남은 채 확인만 닫히는 미스가 없다.
// 예산을 넘긴 정산은 soft claim 뒤 다음 확인이 토큰 CAS로 다시 판정하므로 경고다.
func (r *HandoffReconciler) logOutcome(claim *HandoffClaim, decision handoffcheck.Decision, err error) {
	attrs := []any{
		slog.Int64("id", claim.ID),
		slog.Int("generation", claim.Generation),
		slog.String("observed_action", decision.Action.String()),
		slog.String("reason", decision.Code),
	}

	if err != nil {
		if miss, ok := errors.AsType[*sendfence.MissError](err); ok {
			for _, row := range miss.Rows {
				notificationDeliveryFenceMissTotal.WithLabelValues(miss.Action, string(row.Reason), fenceMissResultFenceLost).Inc()
			}

			r.logger.Warn("notification delivery handoff check lost its claim; not applied", append(attrs, slog.Any("error", err))...)

			return
		}

		if errors.Is(err, context.DeadlineExceeded) {
			r.logger.Warn("notification delivery handoff settlement exceeded its budget; the next check decides again",
				append(attrs, slog.Any("error", err))...)

			return
		}

		r.logger.Error("notification delivery handoff check failed", append(attrs, slog.Any("error", err))...)

		return
	}

	switch decision.Action {
	case handoffcheck.ActionReissue:
		r.logger.Warn("notification delivery handoff failed; requeued with the next request generation", attrs...)
	case handoffcheck.ActionExhausted:
		r.logger.Error("notification delivery handoff failed; reissue budget exhausted", attrs...)
	case handoffcheck.ActionQuarantine:
		r.logger.Error("notification delivery handoff outcome unknown; quarantined", attrs...)
	case handoffcheck.ActionComplete, handoffcheck.ActionReschedule:
	}
}
