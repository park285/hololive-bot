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
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type RequestConsumer interface {
	LoadSendRequest(context.Context, []domain.AlarmQueueEnvelope) (*dispatchoutbox.SendRequest, error)
	PinSendRequest(context.Context, []domain.AlarmQueueEnvelope, dispatchoutbox.SendRequest) (*dispatchoutbox.SendRequest, error)
	ReissueSendRequest(context.Context, []domain.AlarmQueueEnvelope, string) (bool, error)
}

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
	RequestConsumer
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
	PrepareMessageRequest(context.Context, string, string) (string, string, error)
	SendPreparedMessage(context.Context, string, string, string, string) error
	SendMessage(ctx context.Context, roomID, message string) error
}

type Runner struct {
	consumer          Consumer
	sender            Sender
	renderer          *template.Renderer
	messageStrings    *messagestrings.Store
	idleWaiter        IdleWaiter
	shortLinkBaseURL  string
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

	attemptCtx := ctx
	cancel := func() {}

	if r.attemptTimeout > 0 {
		attemptCtx, cancel = context.WithTimeout(ctx, r.attemptTimeout)
	}

	defer cancel()

	err = r.dispatchGroups(attemptCtx, groupAlarmDispatchEnvelopesForDelivery(envelopes))
	if err != nil {
		return true, fmt.Errorf("dispatch alarm dispatch groups: %w", err)
	}

	return true, nil
}

func dispatchAttemptOutcome(err error) workercontract.AttemptOutcome {
	switch {
	case err == nil:
		return workercontract.AttemptSuccess
	case sendoutcome.Classify(err) == sendoutcome.OutcomeUnknown:
		return workercontract.AttemptOutcomeUnknown
	case errors.Is(err, context.DeadlineExceeded):
		return workercontract.AttemptTimeout
	case errors.Is(err, context.Canceled):
		return workercontract.AttemptCanceled
	case sendoutcome.Classify(err) == sendoutcome.TransportAmbiguous:
		return workercontract.AttemptOutcomeUnknown
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

	request, err := r.prepareGroupRequest(ctx, group)
	if err != nil {
		if errors.Is(err, dispatchoutbox.ErrLegacySendRequest) || errors.Is(err, dispatchoutbox.ErrSendRequestFence) {
			return r.withStateContext(ctx, func(stateCtx context.Context) error {
				return r.consumer.Quarantine(stateCtx, group.envelopes, err)
			})
		}

		return r.routePreSendFailure(ctx, group.envelopes, err)
	}

	// claim 이후 다른 세대를 보내지 않도록 저장된 ID를 다시 확인한다.
	if request.ClientRequestID != clientRequestID || request.RoomID != group.roomID {
		return fmt.Errorf("prepare alarm request: %w", dispatchoutbox.ErrSendRequestFence)
	}

	return r.dispatchPreparedMessageGroup(ctx, group, request)
}

func (r *Runner) prepareGroupRequest(ctx context.Context, group alarmDispatchGroup) (*dispatchoutbox.SendRequest, error) {
	request, err := r.consumer.LoadSendRequest(ctx, group.envelopes)
	if err == nil {
		return request, nil
	}

	if !errors.Is(err, dispatchoutbox.ErrSendRequestUnpinned) {
		return nil, fmt.Errorf("load immutable request: %w", err)
	}

	message, err := renderAlarmDispatchGroup(ctx, r.renderer, r.messageStrings, r.members, r.shortLinkBaseURL, group)
	if err != nil {
		return nil, fmt.Errorf("render request: %w", err)
	}

	body, route, err := r.sender.PrepareMessageRequest(ctx, group.roomID, message)
	if err != nil {
		return nil, fmt.Errorf("prepare request route: %w", err)
	}

	request, err = r.consumer.PinSendRequest(ctx, group.envelopes, dispatchoutbox.SendRequest{Body: body, Route: route, RoomID: group.roomID})
	if err != nil {
		return nil, fmt.Errorf("pin immutable request: %w", err)
	}

	return request, nil
}

func (r *Runner) dispatchPreparedMessageGroup(ctx context.Context, group alarmDispatchGroup, request *dispatchoutbox.SendRequest) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("before alarm send: %w", err)
	}

	if proceed, markErr := r.markSending(ctx, group.envelopes); !proceed {
		return markErr
	}

	if sendErr := r.sendPreparedRequest(ctx, request); sendErr != nil {
		group.request = request
		return r.routePostSendingFailure(ctx, group, sendErr)
	}

	return r.markDispatched(ctx, group.envelopes)
}

// 외부 provider 호출마다 한 번만 attempt를 기록한다. DB 상태 반영 결과는 발송 결론을 바꾸지 않는다.
func (r *Runner) sendPreparedRequest(ctx context.Context, request *dispatchoutbox.SendRequest) (err error) {
	id := r.workerTracker.BeginAttempt(time.Now())
	completed := false

	defer func() {
		r.workerTracker.EndAttempt(id)

		outcome := workercontract.AttemptPanic

		if completed {
			outcome = dispatchAttemptOutcome(err)
		}

		r.workerTotals.RecordAttempt(outcome)
	}()

	err = r.sender.SendPreparedMessage(ctx, request.RoomID, request.Body, request.Route, request.ClientRequestID)
	completed = true

	if err != nil {
		return fmt.Errorf("send prepared alarm request: %w", err)
	}

	return nil
}
