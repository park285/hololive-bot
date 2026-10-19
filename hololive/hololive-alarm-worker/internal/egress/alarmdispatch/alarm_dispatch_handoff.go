package alarmdispatch

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

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

// 시간 예산: claim은 단위의 다음 확인을 soft claim(15s) 뒤로 미루고, 단위마다 상태 조회(최대 5s)와 정산(최대
// alarmDispatchStateTimeout 5s, 정산 슬롯 대기·연결 획득·문장 포함)을 차례로 끝낸다. 조회와 정산 예산의 합 10s가
// soft claim보다 짧으므로 같은 단위를 다른 확인이 고르기 전에 이 확인의 적용이 끝나거나 포기된다. 예산을 넘긴
// 정산은 적용하지 않고, soft claim 뒤 다음 확인이 토큰 CAS로 다시 판정한다. DLQ commit 뒤 claim 키 해제는 확인이
// 닫힌 뒤라 soft claim과 무관하므로 정산 예산 밖의 별도 예산을 쓴다.
const (
	// 한 batch의 상태 조회는 모두 병렬로 돈다. 동시 외부 조회 수의 상한이다.
	alarmDispatchHandoffBatchSize     = 32
	alarmDispatchHandoffSoftClaim     = 15 * time.Second
	alarmDispatchHandoffStatusTimeout = 5 * time.Second
	// 정산은 일반 연결 획득으로 단일 문장 하나씩이다. 동시에 쓰는 연결을 이 수로 묶어 batch 정산이 alarm_worker 풀
	// (기본 8, control 예약 1)의 일반 용량을 독점하지 않게 한다. 슬롯 대기까지 포함해 정산 예산을 넘긴 정산은 적용하지
	// 않고, soft claim 뒤 다음 확인이 다시 판정한다.
	alarmDispatchHandoffSettleConcurrency = 2
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

// ReplyStatusClient는 Iris /reply-status 조회다.
type ReplyStatusClient interface {
	GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error)
}

// HandoffReconcilerConfig의 Horizon은 alarm_dispatch executor attempt timeout이고 PollInterval은 alarm_dispatch
// poll 주기다.
type HandoffReconcilerConfig struct {
	Horizon      time.Duration
	PollInterval time.Duration
}

// HandoffReconciler는 Iris admission을 마친 sent send unit의 /reply-status를 확인 시각마다 조회해 정산한다. 외부
// 조회는 트랜잭션 밖에서 하고, 정산은 claim 토큰과 요청 ID가 그대로일 때만 단위 하나의 단일 문장으로 적용한다.
type HandoffReconciler struct {
	consumer     HandoffConsumer
	client       ReplyStatusClient
	horizon      time.Duration
	pollInterval time.Duration
	settleSlots  chan struct{}
	logger       *slog.Logger
}

// NewHandoffReconciler는 지평과 poll 주기가 1ms 이상이어야 만든다.
func NewHandoffReconciler(consumer HandoffConsumer, client ReplyStatusClient, config HandoffReconcilerConfig, logger *slog.Logger) (*HandoffReconciler, error) {
	if consumer == nil || client == nil {
		return nil, errors.New("new alarm dispatch handoff reconciler: consumer and reply status client are required")
	}

	if config.Horizon < time.Millisecond || config.PollInterval < time.Millisecond {
		return nil, fmt.Errorf("new alarm dispatch handoff reconciler: horizon %s and poll interval %s must be at least 1ms", config.Horizon, config.PollInterval)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &HandoffReconciler{
		consumer:     consumer,
		client:       client,
		horizon:      config.Horizon,
		pollInterval: config.PollInterval,
		settleSlots:  make(chan struct{}, alarmDispatchHandoffSettleConcurrency),
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
			r.logger.Warn("alarm dispatch handoff claim failed", slog.Any("error", err))
		}

		if err == nil && claimed == alarmDispatchHandoffBatchSize {
			continue
		}

		if !retry.Sleep(ctx, r.pollInterval) {
			return nil
		}
	}
}

// RunOnce는 확인 시각이 된 단위를 한 batch 고르고, 단위마다 상태 조회와 정산을 병렬로 끝낸 뒤 고른 수를 돌려준다.
// 확인 claim은 batch 조회라 일반 연결 획득을 쓴다.
func (r *HandoffReconciler) RunOnce(ctx context.Context) (int, error) {
	claimCtx, cancel := context.WithTimeout(ctx, alarmDispatchStateTimeout)
	claims, err := r.consumer.ClaimHandoffDue(claimCtx, alarmDispatchHandoffBatchSize, alarmDispatchHandoffSoftClaim)

	cancel()

	if err != nil {
		return 0, fmt.Errorf("claim alarm dispatch handoff checks: %w", err)
	}

	var wg sync.WaitGroup

	for i := range claims {
		wg.Go(func() {
			panicguard.Run(r.logger, panicguard.BackgroundTask, "alarm-dispatch-handoff-check", func() {
				r.reconcile(ctx, &claims[i])
			})
		})
	}

	wg.Wait()

	return len(claims), nil
}

func (r *HandoffReconciler) reconcile(ctx context.Context, claim *dispatchoutbox.HandoffClaim) {
	statusCtx, cancel := context.WithTimeout(ctx, alarmDispatchHandoffStatusTimeout)
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
		RetryBudgetLeft: claim.AttemptCount+1 < alarmDispatchMaxAttempts,
	})

	// 관측을 얻은 뒤의 정산은 종료 취소와 분리하되, 정산 슬롯 대기까지 포함한 예산 안에서만 한다.
	stateCtx, cancelState := context.WithTimeout(context.WithoutCancel(ctx), alarmDispatchStateTimeout)
	defer cancelState()

	err := r.settle(stateCtx, claim, decision, queryErr)

	r.logOutcome(claim, decision, err)
}

// settle은 정산 슬롯을 얻어 관측을 적용한다. 예산 안에 슬롯을 얻지 못하면 적용하지 않는다.
func (r *HandoffReconciler) settle(ctx context.Context, claim *dispatchoutbox.HandoffClaim, decision handoffcheck.Decision, queryErr error) error {
	select {
	case r.settleSlots <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("wait alarm dispatch handoff settlement slot: %w", ctx.Err())
	}

	defer func() { <-r.settleSlots }()

	return r.apply(ctx, claim, decision, queryErr)
}

func (r *HandoffReconciler) apply(ctx context.Context, claim *dispatchoutbox.HandoffClaim, decision handoffcheck.Decision, queryErr error) error {
	var err error

	switch decision.Action {
	case handoffcheck.ActionComplete:
		err = r.consumer.CompleteHandoff(ctx, claim)
	case handoffcheck.ActionReschedule:
		if queryErr != nil {
			// SDK 오류 원문은 /reply-status/{requestId} 경로를 담으므로 HTTP 상태와 오류 class만 남긴다.
			r.logger.Warn("alarm dispatch handoff status query deferred",
				slog.Int64("send_unit_id", claim.UnitID),
				slog.String("error_class", dispatchoutbox.ClassifyErrorCode(queryErr)),
				slog.String("error", handoffcheck.QueryErrorDetail(queryErr)))
		}

		err = r.consumer.RescheduleHandoff(ctx, claim, handoffcheck.NextStep(claim.Elapsed), r.horizon)
	case handoffcheck.ActionReissue:
		err = r.consumer.ReissueAfterHandoff(ctx, claim, alarmDispatchRetryBackoff(claim.AttemptCount+1), decision.Code, decision.Detail)
	case handoffcheck.ActionExhausted:
		err = r.consumer.DLQAfterHandoff(ctx, claim, decision.Code, decision.Detail)
	case handoffcheck.ActionQuarantine:
		err = r.consumer.QuarantineAfterHandoff(ctx, claim, decision.Code, decision.Detail)
	default:
		return fmt.Errorf("apply alarm dispatch handoff: unsupported action %d", decision.Action)
	}

	if err != nil {
		return fmt.Errorf("apply alarm dispatch handoff: %w", err)
	}

	return nil
}

// logOutcome은 정산 결과를 남긴다. 소유 상실 미스는 다음 확인이나 다른 정산이 이어받으므로 경고다. 확인을 닫은 미스는
// 그 단위의 확인이 영구히 끝나 관측 결과가 원장에 반영되지 않았으므로 관측 행동·코드와 함께 오류로 남긴다. 예산을
// 넘긴 정산은 soft claim 뒤 다음 확인이 토큰 CAS로 다시 판정하므로 경고다. DLQ commit 뒤 claim 키 해제 실패는 확인이
// 닫혀 다시 시도되지 않으므로 deadline이어도 오류다. 미스의 원인별 지표와 멤버별 상세 로그는 저장소가 남긴다.
func (r *HandoffReconciler) logOutcome(claim *dispatchoutbox.HandoffClaim, decision handoffcheck.Decision, err error) {
	attrs := []any{
		slog.Int64("send_unit_id", claim.UnitID),
		slog.Int("generation", claim.Generation),
		slog.String("observed_action", decision.Action.String()),
		slog.String("reason", decision.Code),
	}

	if err != nil {
		if errors.Is(err, dispatchoutbox.ErrHandoffCheckClosed) {
			r.logger.Error("alarm dispatch handoff check closed without transition; send unit members changed outside the reconciler",
				append(attrs, slog.Any("error", err))...)

			return
		}

		if _, ok := errors.AsType[*sendfence.MissError](err); ok {
			r.logger.Warn("alarm dispatch handoff check lost its claim or send unit; not applied", append(attrs, slog.Any("error", err))...)

			return
		}

		if errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, dispatchoutbox.ErrHandoffClaimKeysNotReleased) {
			r.logger.Warn("alarm dispatch handoff settlement exceeded its budget; the next check decides again",
				append(attrs, slog.Any("error", err))...)

			return
		}

		r.logger.Error("alarm dispatch handoff check failed", append(attrs, slog.Any("error", err))...)

		return
	}

	switch decision.Action {
	case handoffcheck.ActionReissue:
		r.logger.Warn("alarm dispatch handoff failed; requeued with the next request generation", attrs...)
	case handoffcheck.ActionExhausted:
		r.logger.Error("alarm dispatch handoff failed; reissue budget exhausted", attrs...)
	case handoffcheck.ActionQuarantine:
		r.logger.Error("alarm dispatch handoff outcome unknown; quarantined", attrs...)
	case handoffcheck.ActionComplete, handoffcheck.ActionReschedule:
	}
}
