package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffloop"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
)

type handoffStore interface {
	handoffClaimer
	handoffSettler
}

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

// handoffReconcilerConfig의 Horizon은 youtube_delivery profile handoff_horizon_ms(HandoffHorizon), PollInterval은
// youtube_delivery poll 주기, MaxRetries는 발송 실패 정책과 같은 시도 예산이다.
type handoffReconcilerConfig struct {
	Horizon      time.Duration
	PollInterval time.Duration
	MaxRetries   int
}

// HandoffReconciler는 Iris admission을 마친 youtube 고정 요청의 handoff 확인 loop다. 정산은 claim 토큰·requestId·요청
// 세대·본문 해시·고정 멤버와 멤버 행 버전이 그대로일 때만 요청 하나의 트랜잭션으로 적용한다.
type HandoffReconciler = handoffloop.Reconciler[store.HandoffClaim]

// NewHandoffReconciler는 youtube_delivery dispatcher와 같은 설정으로 전이 저장소를 만든다. 지평은 HandoffHorizon이다.
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
			Horizon: validConfig.HandoffHorizon, PollInterval: validConfig.PollInterval, MaxRetries: validConfig.MaxRetries,
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
	if handoffs == nil {
		return nil, errors.New("new youtube handoff reconciler: store is required")
	}

	if config.MaxRetries <= 0 {
		return nil, fmt.Errorf("new youtube handoff reconciler: max retries %d must be positive", config.MaxRetries)
	}

	reconciler, err := handoffloop.New[store.HandoffClaim](handoffLedger{handoffStore: handoffs, maxRetries: config.MaxRetries}, client, handoffloop.Config{
		Name:               "youtube delivery",
		Horizon:            config.Horizon,
		PollInterval:       config.PollInterval,
		FenceMissMessage:   "youtube delivery handoff check lost its claim or logical group changed; not applied",
		CheckClosed:        store.ErrHandoffCheckClosed,
		CheckClosedMessage: "youtube delivery handoff check closed; frozen members changed since admission",
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("new youtube handoff reconciler: %w", err)
	}

	return reconciler, nil
}

// handoffLedger는 고정 요청 확인을 handoffloop에 잇는다. 정산마다 lifecycle 지표를 남기고, 적용되지 않은 결과는 오류로
// 돌려주며, 적용된 실패 정산이 바꾼 원장 수를 남긴다.
type handoffLedger struct {
	handoffStore

	maxRetries int
}

func (l handoffLedger) Check(claim *store.HandoffClaim) handoffloop.Check {
	return handoffloop.Check{
		IrisRequestID:   claim.IrisRequestID,
		Elapsed:         claim.Elapsed,
		Generation:      claim.Generation,
		RetryBudgetLeft: claim.AttemptCount+1 < l.maxRetries,
		Subject:         slog.Any("delivery_ids", claim.MemberIDs),
	}
}

func (l handoffLedger) Complete(ctx context.Context, claim *store.HandoffClaim) error {
	result, err := l.CompleteHandoff(ctx, claim)

	return settledHandoff(handoffcheck.ActionComplete, claim, result, err)
}

func (l handoffLedger) Reschedule(ctx context.Context, claim *store.HandoffClaim, step, horizon time.Duration) error {
	result, err := l.RescheduleHandoff(ctx, claim, step, horizon)

	return settledHandoff(handoffcheck.ActionReschedule, claim, result, err)
}

func (l handoffLedger) Reissue(ctx context.Context, claim *store.HandoffClaim, code, detail string) error {
	result, err := l.ReissueAfterHandoff(ctx, claim, code, detail)

	return settledHandoff(handoffcheck.ActionReissue, claim, result, err)
}

func (l handoffLedger) Exhaust(ctx context.Context, claim *store.HandoffClaim, code, detail string) error {
	result, err := l.ExhaustAfterHandoff(ctx, claim, code, detail)

	return settledHandoff(handoffcheck.ActionExhausted, claim, result, err)
}

func (l handoffLedger) Quarantine(ctx context.Context, claim *store.HandoffClaim, code, detail string) error {
	result, err := l.QuarantineAfterHandoff(ctx, claim, code, detail)

	return settledHandoff(handoffcheck.ActionQuarantine, claim, result, err)
}

func settledHandoff(action handoffcheck.Action, claim *store.HandoffClaim, result store.ApplyResult, err error) error {
	units := len(claim.MemberIDs)

	observeLifecycleApply("handoff_"+action.String(), result, err, units)

	if err != nil {
		return err
	}

	if result.Outcome != store.ApplyApplied {
		return fmt.Errorf("outcome %s", result.Outcome)
	}

	observeHandoffLedger(action, result, units)

	return nil
}

// observeHandoffLedger는 실패 정산이 지운 SENT 원장과 격리가 기록한 QUARANTINED 원장 수를 남긴다. 적용된 정산은 고정
// 멤버 하나당 논리 키 하나를 바꾼다.
func observeHandoffLedger(action handoffcheck.Action, result store.ApplyResult, units int) {
	switch action {
	case handoffcheck.ActionReissue, handoffcheck.ActionExhausted:
		observeLedgerOperation("handoff_delete_sent", result, units)
	case handoffcheck.ActionQuarantine:
		observeLedgerOperation("handoff_delete_sent", result, units)
		observeLedgerOperation("record_quarantined", result, units)
	case handoffcheck.ActionComplete, handoffcheck.ActionReschedule:
	}
}
