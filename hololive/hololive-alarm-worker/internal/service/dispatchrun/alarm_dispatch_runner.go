package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type QueueConsumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	ReleaseClaimKeys(ctx context.Context, claimKeys []string) error
}

type FailureRouter interface {
	RouteFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error
	RouteSendingFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error
	RequeuePreSend(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error
}

type Consumer interface {
	QueueConsumer
	FailureRouter
}

var _ Consumer = (*dispatchoutbox.Consumer)(nil)

type IdleWaiter interface {
	Wait(ctx context.Context) bool
	Reset()
}

// Sender는 alarm dispatch가 쓰는 Text 발송 계약이다. 오픈채팅의 Markdown 선택은 sender가 방 유형으로 정한다.
// Karing template은 보내지 않는다(DEC-20260926-hololive-karing-egress-disposition).
type Sender interface {
	SendMessage(ctx context.Context, roomID, message string) error
}

type clientRequestSender interface {
	SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error
}

type Runner struct {
	consumer          Consumer
	sender            Sender
	renderer          *template.Renderer
	messageStrings    *messagestrings.Store
	idleWaiter        IdleWaiter
	shortLinkBaseURL  string
	seeMoreFold       bool
	maxBatch          int
	maxBatchesPerWake int
	batchesSinceWake  int
	yield             func(context.Context) bool
	logger            *slog.Logger
	members           domain.MemberDataProvider
	attemptTimeout    time.Duration
	workerTracker     *workercontract.ExecutorTracker
	workerTotals      *workercontract.Counters
}

type RunnerConfig struct {
	ShortLinkBaseURL  string
	SeeMoreFold       bool // 여러 항목 묶음 텍스트 알림을 '전체보기'로 접는다(BOT_SEE_MORE_FOLD).
	MaxBatch          int
	MaxBatchesPerWake int
	Members           domain.MemberDataProvider
	AttemptTimeout    time.Duration
	WorkerTracker     *workercontract.ExecutorTracker
	WorkerTotals      *workercontract.Counters
}

func NewRunner(
	consumer Consumer,
	sender Sender,
	renderer *template.Renderer,
	messageStrings *messagestrings.Store,
	idleWaiter IdleWaiter,
	config RunnerConfig,
	logger *slog.Logger,
) *Runner {
	return &Runner{
		consumer:          consumer,
		sender:            sender,
		renderer:          renderer,
		messageStrings:    messageStrings,
		idleWaiter:        idleWaiter,
		shortLinkBaseURL:  config.ShortLinkBaseURL,
		seeMoreFold:       config.SeeMoreFold,
		maxBatch:          config.MaxBatch,
		maxBatchesPerWake: config.MaxBatchesPerWake,
		logger:            logger,
		members:           config.Members,
		attemptTimeout:    config.AttemptTimeout,
		workerTracker:     config.WorkerTracker,
		workerTotals:      config.WorkerTotals,
	}
}

func (r *Runner) runOnce(ctx context.Context) (bool, error) {
	envelopes, err := r.consumer.DrainBatch(ctx, r.maxBatch)
	if err != nil {
		return false, fmt.Errorf("drain alarm dispatch batch: %w", err)
	}

	if len(envelopes) == 0 {
		return false, nil
	}

	attemptID := r.workerTracker.BeginAttempt(time.Now())

	defer r.workerTracker.EndAttempt(attemptID)

	attemptCtx := ctx
	cancel := func() {}

	if r.attemptTimeout > 0 {
		attemptCtx, cancel = context.WithTimeout(ctx, r.attemptTimeout)
	}

	defer cancel()

	err = r.dispatchGroups(attemptCtx, groupAlarmDispatchEnvelopesForDelivery(envelopes))
	r.workerTotals.RecordAttempt(dispatchAttemptOutcome(err))

	if err != nil {
		return true, fmt.Errorf("dispatch alarm dispatch groups: %w", err)
	}

	return true, nil
}

func dispatchAttemptOutcome(err error) workercontract.AttemptOutcome {
	switch {
	case err == nil:
		return workercontract.AttemptSuccess
	case errors.Is(err, context.DeadlineExceeded):
		return workercontract.AttemptTimeout
	case errors.Is(err, context.Canceled):
		return workercontract.AttemptCanceled
	default:
		return workercontract.AttemptFailed
	}
}

func (r *Runner) dispatchGroups(ctx context.Context, groups []alarmDispatchGroup) error {
	for _, group := range groups {
		// 만료된 attempt로 남은 그룹을 계속 보내면 즉시 실패한 미발송 행이 sending으로 전이돼
		// 재시도 대신 quarantine으로 굳는다. 남은 그룹은 leased로 두고 다음 드레인에 맡긴다.
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("dispatch alarm groups: %w", err)
		}

		if err := r.dispatchGroup(ctx, group); err != nil {
			return fmt.Errorf("dispatch group: %w", err)
		}
	}

	return nil
}

const alarmDispatchStateTimeout = 5 * time.Second

// 상태 기록과 실패 라우팅은 발송 attempt가 끝난 뒤에도 완료돼야 한다. 이 attempt의 deadline이나
// 종료 신호로 같이 끊기면 드레인된 행이 sending으로 남아 terminal quarantine으로 굳는다.
func (r *Runner) withStateContext(ctx context.Context, fn func(context.Context) error) error {
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alarmDispatchStateTimeout)
	defer cancel()

	if err := fn(stateCtx); err != nil {
		return fmt.Errorf("fn: %w", err)
	}

	return nil
}

func (r *Runner) markSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (proceed bool, err error) {
	markErr := r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.consumer.MarkSending(stateCtx, envelopes)
	})
	if markErr == nil {
		return true, nil
	}

	if err := r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistMarkSendingFailure(stateCtx, envelopes, markErr)
	}); err != nil {
		return false, fmt.Errorf("with state context: %w", err)
	}

	return false, nil
}

func (r *Runner) markDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if err := r.withStateContext(ctx, func(stateCtx context.Context) error {
		if err := r.consumer.MarkDispatched(stateCtx, envelopes); err != nil {
			return fmt.Errorf("mark alarm dispatch sent: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("with state context: %w", err)
	}

	return nil
}

func (r *Runner) routePreSendFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	if err := r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistPreSendFailure(stateCtx, envelopes, cause)
	}); err != nil {
		return fmt.Errorf("with state context: %w", err)
	}

	return nil
}

func (r *Runner) routePostSendingFailure(ctx context.Context, group alarmDispatchGroup, cause error) error {
	if err := r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistPostSendingFailure(stateCtx, group, cause)
	}); err != nil {
		return fmt.Errorf("with state context: %w", err)
	}

	return nil
}

func (r *Runner) dispatchGroup(ctx context.Context, group alarmDispatchGroup) error {
	if err := alarmDispatchGroupError(group); err != nil {
		if errors.Is(err, errAlarmDispatchRetiredStreamProvider) && r.logger != nil {
			// 드레인 표시: 퇴역 제공자 봉투가 아직 남아 있다는 신호다. 제거 조건 확인 때 이 로그가 0건이어야 한다.
			r.logger.Error("alarm dispatch drained a retired stream provider envelope",
				slog.String("room_id", group.roomID),
				slog.Int("envelopes", len(group.envelopes)))
		}

		if routeErr := r.routePreSendFailure(ctx, group.envelopes, err); routeErr != nil {
			return fmt.Errorf("route invalid egress group: %w", routeErr)
		}

		return nil
	}

	if err := r.dispatchMessageGroup(ctx, group); err != nil {
		return fmt.Errorf("dispatch message group: %w", err)
	}

	return nil
}

func (r *Runner) dispatchMessageGroup(ctx context.Context, group alarmDispatchGroup) error {
	clientRequestID, err := alarmDispatchClientRequestID(group)
	if err != nil {
		// 드레인 종단이 아니라 위반 표시다. 운영에서 보이면 저장된 send-unit 식별자가 비었거나 섞인 그룹이 claim됐다는 뜻이다.
		if r.logger != nil {
			r.logger.Error("alarm dispatch group lacks a persisted send unit; not sending",
				slog.String("room_id", group.roomID),
				slog.Int("envelopes", len(group.envelopes)),
				slog.Any("error", err))
		}

		if routeErr := r.routePreSendFailure(ctx, group.envelopes, err); routeErr != nil {
			return fmt.Errorf("route missing send unit identity: %w", routeErr)
		}

		return nil
	}

	message, err := renderAlarmDispatchGroup(ctx, r.renderer, r.messageStrings, r.members, r.shortLinkBaseURL, r.seeMoreFold, group)
	if err != nil {
		if routeErr := r.routePreSendFailure(ctx, group.envelopes, err); routeErr != nil {
			return fmt.Errorf("route pre send failure: %w", routeErr)
		}

		return nil
	}

	if err := r.dispatchRenderedMessageGroup(ctx, group, message, clientRequestID); err != nil {
		return fmt.Errorf("dispatch rendered message group: %w", err)
	}

	return nil
}

func (r *Runner) dispatchRenderedMessageGroup(ctx context.Context, group alarmDispatchGroup, message, clientRequestID string) error {
	// markSending은 실패를 영속화까지 마치면 err 없이 proceed=false를 돌려준다. nil을 감싸면
	// 정상적인 발송 중단이 루프 오류로 바뀐다.
	if proceed, markErr := r.markSending(ctx, group.envelopes); !proceed {
		if markErr != nil {
			return fmt.Errorf("mark alarm dispatch sending: %w", markErr)
		}

		return nil
	}

	if sendErr := sendAlarmDispatchMessage(ctx, r.sender, group, message, clientRequestID); sendErr != nil {
		if routeErr := r.routePostSendingFailure(ctx, group, sendErr); routeErr != nil {
			return fmt.Errorf("route post sending failure: %w", routeErr)
		}

		return nil
	}

	if err := r.markDispatched(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark dispatched: %w", err)
	}

	return nil
}

func sendAlarmDispatchMessage(ctx context.Context, sender Sender, group alarmDispatchGroup, message, clientRequestID string) error {
	if idSender, ok := sender.(clientRequestSender); ok {
		if err := idSender.SendMessageWithClientRequestID(ctx, group.roomID, message, clientRequestID); err != nil {
			return fmt.Errorf("send message with client request ID: %w", err)
		}

		return nil
	}

	if err := sender.SendMessage(ctx, group.roomID, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}
