package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// RequestConsumer의 fence 미스는 *sendfence.MissError이고, 재발급 세대 소진은 sendfence.ErrGenerationExhausted다.
type RequestConsumer interface {
	LoadSendRequest(context.Context, []domain.AlarmQueueEnvelope) (*dispatchoutbox.SendRequest, error)
	PinSendRequest(context.Context, []domain.AlarmQueueEnvelope, dispatchoutbox.SendRequest) (*dispatchoutbox.SendRequest, error)
	ReissueSendRequest(context.Context, []domain.AlarmQueueEnvelope, *dispatchoutbox.SendRequest) error
}

type QueueConsumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, irisRequestID string) error
	ReleaseLeased(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
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

type Runner struct {
	consumer          Consumer
	sender            delivery.PreparedMessageSender
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
	sender delivery.PreparedMessageSender,
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

	ctx, batchSpan := otel.Tracer("hololive/alarm").Start(ctx, "alarm.dispatch.batch")
	defer batchSpan.End()

	batchSpan.SetAttributes(attribute.Int("dispatch.envelopes", len(envelopes)))

	err = r.dispatchGroups(ctx, groupAlarmDispatchEnvelopesForDelivery(envelopes))
	if err != nil {
		batchSpan.SetStatus(codes.Error, "batch settlement failed")

		return true, fmt.Errorf("dispatch alarm dispatch groups: %w", err)
	}

	// 배치 정산 성공과 개별 외부 전송 결과는 별도 span으로 구분한다.
	batchSpan.SetStatus(codes.Ok, "")

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
		// 부모 종료 시 남은 그룹은 leased로 둔다. 각 그룹의 MarkSending은
		// 만료된 소유권을 거부하고 성공한 경우에만 기존 SENDING lease를 부여한다.
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
	stateCtx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(context.WithoutCancel(ctx)), alarmDispatchStateTimeout)
	defer cancel()

	return fn(stateCtx)
}

// markSending은 그룹을 발송 상태에 넣는다. 미스는 routePreSendMiss가 처리하고, 그 밖의 실패는 같은 attempt로
// 재대기시킨다. 반환값 proceed가 참일 때만 외부 호출을 한다.
func (r *Runner) markSending(ctx context.Context, group alarmDispatchGroup) (proceed bool, err error) {
	markErr := r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.consumer.MarkSending(stateCtx, group.envelopes)
	})
	if markErr == nil {
		return true, nil
	}

	if miss, ok := errors.AsType[*sendfence.MissError](markErr); ok {
		return false, r.routePreSendMiss(ctx, group, miss)
	}

	if err := r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistMarkSendingFailure(stateCtx, group.envelopes, markErr)
	}); err != nil {
		return false, err
	}

	return false, nil
}

// routePreSendMiss는 외부 호출 전(Load·Pin·Begin) fence 미스를 처리한다. 이 문장들은 단위 전원이 함께 전이하므로 미스면
// 그룹의 어떤 행도 바뀌지 않았다. 모든 원인이 소유권을 남길 때만 이 claim의 행에 후속 전이를 쓴다.
//   - member_terminal: 고정 멤버가 종료돼 단위를 다시 모을 수 없으므로 격리한다. 같은 요청 ID로 이미 나간 메시지에
//     포함됐을 수 있어 운영 검토 대상이다. 반환하면 claim과 반환이 끝없이 반복된다.
//   - membership_changed: 미발송 claim이므로 격리하지 않고 반환해 다음 claim이 단위 전원을 다시 모으게 한다.
//   - 그 밖(lease 만료, 상태·소유자·attempt 변경, 요청 변경): 회수 경로나 현재 소유자의 몫이므로 손대지 않는다.
func (r *Runner) routePreSendMiss(ctx context.Context, group alarmDispatchGroup, miss *sendfence.MissError) error {
	ownershipHeld := true

	for _, row := range miss.Rows {
		ownershipHeld = ownershipHeld && row.Reason.OwnershipHeld()
	}

	switch {
	case ownershipHeld && miss.Has(sendfence.ReasonMemberTerminal):
		return r.withStateContext(ctx, func(stateCtx context.Context) error {
			if err := r.consumer.Quarantine(stateCtx, group.envelopes, miss); err != nil {
				return fmt.Errorf("quarantine alarm dispatch with terminal send unit member: %w", err)
			}

			return nil
		})
	case ownershipHeld && miss.Has(sendfence.ReasonMembershipChanged):
		return r.withStateContext(ctx, func(stateCtx context.Context) error {
			if err := r.consumer.ReleaseLeased(stateCtx, group.envelopes); err != nil {
				return fmt.Errorf("release alarm dispatch with changed send unit membership: %w", err)
			}

			return nil
		})
	default:
		if r.logger != nil {
			r.logger.Warn("alarm dispatch claim lost before send; leaving rows to their owner",
				privacylog.RoomIDAttr(group.roomID),
				slog.Int("envelopes", len(group.envelopes)),
				slog.Any("error", miss))
		}

		return nil
	}
}

func (r *Runner) markDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, irisRequestID string) error {
	return r.withStateContext(ctx, func(stateCtx context.Context) error {
		if err := r.consumer.MarkDispatched(stateCtx, envelopes, irisRequestID); err != nil {
			return fmt.Errorf("mark alarm dispatch sent: %w", err)
		}

		return nil
	})
}

func (r *Runner) routePreSendFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	return r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistPreSendFailure(stateCtx, envelopes, cause)
	})
}

func (r *Runner) routePostSendingFailure(ctx context.Context, group alarmDispatchGroup, cause error) error {
	return r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistPostSendingFailure(stateCtx, group, cause)
	})
}

func (r *Runner) dispatchGroup(ctx context.Context, group alarmDispatchGroup) error {
	if err := alarmDispatchGroupError(group); err != nil {
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
	if _, err := alarmDispatchClientRequestID(group); err != nil {
		// 드레인 종단이 아니라 위반 표시다. 운영에서 보이면 저장된 send-unit 식별자가 비었거나 섞인 그룹이 claim됐다는 뜻이다.
		if r.logger != nil {
			r.logger.Error("alarm dispatch group lacks a persisted send unit; not sending",
				privacylog.RoomIDAttr(group.roomID),
				slog.Int("envelopes", len(group.envelopes)),
				slog.Any("error", err))
		}

		if routeErr := r.routePreSendFailure(ctx, group.envelopes, err); routeErr != nil {
			return fmt.Errorf("route missing send unit identity: %w", routeErr)
		}

		return nil
	}

	// 요청 읽기·고정 fence는 claim에서 본 client_request_id까지 확인하므로 다른 세대를 보내지 않는다.
	request, err := r.prepareGroupRequest(ctx, group)
	if err != nil {
		if miss, ok := errors.AsType[*sendfence.MissError](err); ok {
			return r.routePreSendMiss(ctx, group, miss)
		}

		return r.routePreSendFailure(ctx, group.envelopes, err)
	}

	if request.RoomID != group.roomID {
		return errors.New("prepare alarm request: stored send unit room differs from the claimed group")
	}

	return r.dispatchPreparedMessageGroup(ctx, group, request)
}

func (r *Runner) prepareGroupRequest(ctx context.Context, group alarmDispatchGroup) (prepared *dispatchoutbox.SendRequest, resultErr error) {
	ctx, span := otel.Tracer("hololive/alarm").Start(ctx, "alarm.dispatch.prepare")

	defer func() {
		if resultErr != nil {
			span.SetStatus(codes.Error, "request preparation failed")
		} else {
			span.SetStatus(codes.Ok, "")
		}

		span.End()
	}()

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

	if proceed, markErr := r.markSending(ctx, group); !proceed {
		return markErr
	}

	// MarkSending의 분리된 상태 컨텍스트가 성공해도 발송 컨텍스트는 이미 끝났을 수 있다.
	// provider 호출 전이므로 같은 attempt와 소유권 fence로 sending을 재대기시킨다.
	if err := ctx.Err(); err != nil {
		return r.withStateContext(ctx, func(stateCtx context.Context) error {
			return r.persistMarkSendingFailure(stateCtx, group.envelopes, err)
		})
	}

	irisRequestID, sendErr := r.sendPreparedRequest(ctx, request)
	if sendErr != nil {
		group.request = request
		return r.routePostSendingFailure(ctx, group, sendErr)
	}

	return r.markDispatched(ctx, group.envelopes, irisRequestID)
}

// 외부 provider 호출마다 한 번만 attempt를 기록한다. DB 상태 반영 결과는 발송 결론을 바꾸지 않는다. 성공이면 Iris가
// admission을 증명한 requestId를 돌려준다.
func (r *Runner) sendPreparedRequest(ctx context.Context, request *dispatchoutbox.SendRequest) (irisRequestID string, err error) {
	if r.attemptTimeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, r.attemptTimeout)

		defer cancel()
	}

	ctx, span := otel.Tracer("hololive/alarm").Start(ctx, "iris.reply.send")
	id := r.workerTracker.BeginAttempt(time.Now())
	completed := false

	defer func() {
		r.workerTracker.EndAttempt(id)

		outcome := workercontract.AttemptPanic

		if completed {
			outcome = dispatchAttemptOutcome(err)
		}

		r.workerTotals.RecordAttempt(outcome)
		span.SetAttributes(attribute.String("delivery.outcome", string(outcome)))

		if outcome == workercontract.AttemptSuccess {
			span.SetStatus(codes.Ok, "")
		} else {
			span.SetStatus(codes.Error, string(outcome))

			if r.logger != nil {
				r.logger.WarnContext(ctx, "alarm dispatch send attempt finished", slog.String("outcome", string(outcome)))
			}
		}

		span.End()
	}()

	irisRequestID, err = r.sender.SendPreparedMessage(ctx, request.RoomID, request.Body, request.Route, request.ClientRequestID)
	completed = true

	if err != nil {
		return "", fmt.Errorf("send prepared alarm request: %w", err)
	}

	return irisRequestID, nil
}
