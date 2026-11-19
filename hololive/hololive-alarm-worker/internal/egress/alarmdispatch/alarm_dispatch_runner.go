package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type RequestConsumer interface {
	LoadSendRequest(context.Context, []dispatchoutbox.AlarmQueueEnvelope) (*dispatchoutbox.SendRequest, error)
	PinSendRequest(context.Context, []dispatchoutbox.AlarmQueueEnvelope, dispatchoutbox.SendRequest) (*dispatchoutbox.SendRequest, error)
	ReissueSendRequest(context.Context, []dispatchoutbox.AlarmQueueEnvelope, *dispatchoutbox.SendRequest) error
}

type QueueConsumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]dispatchoutbox.AlarmQueueEnvelope, error)
	MarkSending(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error
	MarkDispatched(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, irisRequestID string, handoffHorizon time.Duration) error
	ReleaseLeased(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error
	ReleaseClaimKeys(ctx context.Context, claimKeys []string) error
}

type FailureRouter interface {
	RouteFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []dispatchoutbox.AlarmQueueEnvelope) error
	RouteSendingFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []dispatchoutbox.AlarmQueueEnvelope) error
	RequeuePreSend(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error
	Requeue(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error
	Quarantine(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) error
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
	shortLinks        shortlinkservice.YouTubeBuilder
	maxBatch          int
	maxBatchesPerWake int
	batchesSinceWake  int
	yield             func(context.Context) bool
	logger            *slog.Logger
	members           domain.MemberDataProvider
	attemptTimeout    time.Duration
	handoffHorizon    time.Duration
	workerTracker     *workercontract.ExecutorTracker
	workerTotals      *workercontract.Counters
}

type RunnerConfig struct {
	ShortLinks        shortlinkservice.YouTubeBuilder
	MaxBatch          int
	MaxBatchesPerWake int
	Members           domain.MemberDataProvider
	AttemptTimeout    time.Duration
	HandoffHorizon    time.Duration
	WorkerTracker     *workercontract.ExecutorTracker
	WorkerTotals      *workercontract.Counters
}

var errAlarmDispatchSendUnitRoomMismatch = errors.New("alarm dispatch stored send unit room differs from the claimed group")

func NewRunner(
	consumer Consumer,
	sender delivery.PreparedMessageSender,
	renderer *template.Renderer,
	messageStrings *messagestrings.Store,
	idleWaiter IdleWaiter,
	config RunnerConfig,
	logger *slog.Logger,
) (*Runner, error) {
	if consumer == nil || sender == nil || idleWaiter == nil {
		return nil, errors.New("alarm dispatch runner requires consumer, sender and idle waiter")
	}

	return &Runner{
		consumer:          consumer,
		sender:            sender,
		renderer:          renderer,
		messageStrings:    messageStrings,
		idleWaiter:        idleWaiter,
		shortLinks:        config.ShortLinks,
		maxBatch:          config.MaxBatch,
		maxBatchesPerWake: config.MaxBatchesPerWake,
		logger:            logger,
		members:           config.Members,
		attemptTimeout:    config.AttemptTimeout,
		handoffHorizon:    config.HandoffHorizon,
		workerTracker:     config.WorkerTracker,
		workerTotals:      config.WorkerTotals,
	}, nil
}

func (r *Runner) runOnce(ctx context.Context) (bool, error) {
	envelopes, err := r.consumer.DrainBatch(ctx, r.maxBatch)
	if err != nil {
		return false, fmt.Errorf("drain alarm dispatch batch: %w", err)
	}

	if len(envelopes) == 0 {
		return false, nil
	}

	err = r.dispatchGroups(ctx, groupAlarmDispatchEnvelopesForDelivery(envelopes))
	if err != nil {
		return true, fmt.Errorf("dispatch alarm dispatch groups: %w", err)
	}

	return true, nil
}

func (r *Runner) dispatchGroups(ctx context.Context, groups []alarmDispatchGroup) error {
	members := &alarmDispatchMembers{provider: r.members}

	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("dispatch alarm groups: %w", err)
		}

		if err := r.dispatchGroup(ctx, group, members); err != nil {
			return fmt.Errorf("dispatch group: %w", err)
		}
	}

	return nil
}

const alarmDispatchStateTimeout = 5 * time.Second

func (r *Runner) withStateContext(ctx context.Context, fn func(context.Context) error) error {
	stateCtx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(context.WithoutCancel(ctx)), alarmDispatchStateTimeout)
	defer cancel()

	return fn(stateCtx)
}

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

func (r *Runner) markDispatched(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, irisRequestID string) error {
	return r.withStateContext(ctx, func(stateCtx context.Context) error {
		if err := r.consumer.MarkDispatched(stateCtx, envelopes, irisRequestID, r.handoffHorizon); err != nil {
			return fmt.Errorf("mark alarm dispatch sent: %w", err)
		}

		return nil
	})
}

func (r *Runner) routePreSendFailure(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) error {
	return r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistPreSendFailure(stateCtx, envelopes, cause)
	})
}

func (r *Runner) routePostSendingFailure(ctx context.Context, group alarmDispatchGroup, cause error) error {
	return r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistPostSendingFailure(stateCtx, group, cause)
	})
}

func (r *Runner) dispatchGroup(ctx context.Context, group alarmDispatchGroup, members *alarmDispatchMembers) error {
	if err := alarmDispatchGroupError(group); err != nil {
		if routeErr := r.routePreSendFailure(ctx, group.envelopes, err); routeErr != nil {
			return fmt.Errorf("route invalid egress group: %w", routeErr)
		}

		return nil
	}

	if err := r.dispatchMessageGroup(ctx, group, members); err != nil {
		return fmt.Errorf("dispatch message group: %w", err)
	}

	return nil
}

func (r *Runner) dispatchMessageGroup(ctx context.Context, group alarmDispatchGroup, members *alarmDispatchMembers) error {
	request, err := r.prepareGroupRequest(ctx, group, members)
	if err != nil {
		if miss, ok := errors.AsType[*sendfence.MissError](err); ok {
			return r.routePreSendMiss(ctx, group, miss)
		}

		return r.routePreSendFailure(ctx, group.envelopes, err)
	}

	if request.RoomID != group.roomID {
		if r.logger != nil {
			r.logger.Error("alarm dispatch stored send unit room differs from the claimed group; not sending",
				privacylog.RoomIDAttr(group.roomID),
				slog.Int("envelopes", len(group.envelopes)))
		}

		if routeErr := r.routePreSendFailure(ctx, group.envelopes, errAlarmDispatchSendUnitRoomMismatch); routeErr != nil {
			return fmt.Errorf("route send unit room mismatch: %w", routeErr)
		}

		return nil
	}

	return r.dispatchPreparedMessageGroup(ctx, group, request)
}

func (r *Runner) prepareGroupRequest(ctx context.Context, group alarmDispatchGroup, members *alarmDispatchMembers) (*dispatchoutbox.SendRequest, error) {
	if !slices.ContainsFunc(group.envelopes, func(envelope dispatchoutbox.AlarmQueueEnvelope) bool { return envelope.RequestUnpinned }) {
		request, err := r.consumer.LoadSendRequest(ctx, group.envelopes)
		if err == nil {
			return request, nil
		}

		if !errors.Is(err, dispatchoutbox.ErrSendRequestUnpinned) {
			return nil, fmt.Errorf("load immutable request: %w", err)
		}
	}

	message, err := renderAlarmDispatchGroup(ctx, r.renderer, r.messageStrings, members, r.shortLinks, group)
	if err != nil {
		return nil, fmt.Errorf("render request: %w", err)
	}

	body, route, err := r.sender.PrepareMessageRequest(ctx, group.roomID, message)
	if err != nil {
		return nil, fmt.Errorf("prepare request route: %w", err)
	}

	request, err := r.consumer.PinSendRequest(ctx, group.envelopes, dispatchoutbox.SendRequest{Body: body, Route: route, RoomID: group.roomID})
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

func (r *Runner) sendPreparedRequest(ctx context.Context, request *dispatchoutbox.SendRequest) (irisRequestID string, err error) {
	if r.attemptTimeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, r.attemptTimeout)

		defer cancel()
	}

	id := r.workerTracker.BeginAttempt(time.Now())
	completed := false

	defer func() {
		r.workerTracker.EndAttempt(id)

		outcome := workercontract.AttemptPanic

		if completed {
			outcome = sendoutcome.AttemptOutcome(err, sendoutcome.AttemptRules{})
		}

		r.workerTotals.RecordAttempt(outcome)

		if outcome != workercontract.AttemptSuccess && r.logger != nil {
			r.logger.WarnContext(ctx, "alarm dispatch send attempt finished", slog.String("outcome", string(outcome)))
		}
	}()

	irisRequestID, err = r.sender.SendPreparedMessage(ctx, request.RoomID, request.Body, request.Route, request.ClientRequestID)
	completed = true

	if err != nil {
		return "", fmt.Errorf("send prepared alarm request: %w", err)
	}

	return irisRequestID, nil
}
