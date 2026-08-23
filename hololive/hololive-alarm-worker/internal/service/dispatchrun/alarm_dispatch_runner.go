package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

type Consumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	ReleaseClaimKeys(ctx context.Context, claimKeys []string) error
	RouteFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error
	RouteSendingFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error
	RequeuePreSend(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error
}

var _ Consumer = (*dispatchoutbox.Consumer)(nil)

type IdleWaiter interface {
	Wait(ctx context.Context) bool
	Reset()
}

type Sender interface {
	SendMessage(ctx context.Context, roomID, message string) error
	SendKaringContentList(ctx context.Context, roomID string, req *iris.KaringContentListRequest) error
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
	karingEnabled     bool
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
	KaringEnabled     bool
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
		karingEnabled:     config.KaringEnabled,
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
	err = r.dispatchGroups(attemptCtx, groupAlarmDispatchEnvelopesForKaring(envelopes, r.karingEnabled))
	r.workerTotals.RecordAttempt(dispatchAttemptOutcome(err))
	return true, err
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
			return err
		}
	}
	return nil
}

const alarmDispatchStateTimeout = 5 * time.Second

// 상태 기록과 실패 라우팅은 발송 attempt가 끝난 뒤에도 완료돼야 한다. attempt deadline이나
// 종료 신호로 같이 끊기면 드레인된 행이 sending으로 남아 terminal quarantine으로 굳는다.
func (r *Runner) withStateContext(ctx context.Context, fn func(context.Context) error) error {
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alarmDispatchStateTimeout)
	defer cancel()
	return fn(stateCtx)
}

func (r *Runner) markSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (proceed bool, err error) {
	markErr := r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.consumer.MarkSending(stateCtx, envelopes)
	})
	if markErr == nil {
		return true, nil
	}
	return false, r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistMarkSendingFailure(stateCtx, envelopes, markErr)
	})
}

func (r *Runner) markDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	return r.withStateContext(ctx, func(stateCtx context.Context) error {
		if err := r.consumer.MarkDispatched(stateCtx, envelopes); err != nil {
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

func (r *Runner) routePostSendingFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	return r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistPostSendingFailure(stateCtx, envelopes, cause)
	})
}

func (r *Runner) dispatchGroup(ctx context.Context, group alarmDispatchGroup) error {
	if alarmDispatchGroupUsesTextPath(group) {
		return r.dispatchMessageGroup(ctx, group)
	}
	if !r.karingEnabled {
		return r.dispatchMessageGroup(ctx, group)
	}
	return r.dispatchKaringContentListGroup(ctx, group)
}

func alarmDispatchGroupUsesTextPath(group alarmDispatchGroup) bool {
	if len(group.envelopes) == 0 {
		return false
	}
	envelope := group.envelopes[0]
	if envelope.SourceKind == domain.AlarmDispatchSourceKindCelebration {
		return true
	}
	if envelope.SourceKind == domain.AlarmDispatchSourceKindDeliveryDigest {
		return true
	}
	return envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox &&
		envelope.YouTubeOutbox != nil &&
		envelope.YouTubeOutbox.Kind == domain.OutboxKindMilestone
}

func (r *Runner) dispatchMessageGroup(ctx context.Context, group alarmDispatchGroup) error {
	message, err := renderAlarmDispatchGroup(ctx, r.renderer, r.messageStrings, r.members, group)
	if err != nil {
		return r.routePreSendFailure(ctx, group.envelopes, err)
	}
	if proceed, err := r.markSending(ctx, group.envelopes); !proceed {
		return err
	}
	if err := sendAlarmDispatchMessage(ctx, r.sender, group, message); err != nil {
		return r.routePostSendingFailure(ctx, group.envelopes, err)
	}
	return r.markDispatched(ctx, group.envelopes)
}

func sendAlarmDispatchMessage(ctx context.Context, sender Sender, group alarmDispatchGroup, message string) error {
	if clientRequestSender, ok := sender.(clientRequestSender); ok {
		return clientRequestSender.SendMessageWithClientRequestID(ctx, group.roomID, message, alarmDispatchClientRequestID(group, 0, len(group.envelopes)))
	}
	return sender.SendMessage(ctx, group.roomID, message)
}

func (r *Runner) dispatchKaringContentListGroup(ctx context.Context, group alarmDispatchGroup) error {
	requests, err := buildAlarmDispatchKaringContentListRequests(ctx, r.messageStrings, group)
	if err != nil {
		return r.routePreSendFailure(ctx, group.envelopes, err)
	}
	if proceed, err := r.markSending(ctx, group.envelopes); !proceed {
		return err
	}
	for i := range requests {
		if err := r.sender.SendKaringContentList(ctx, group.roomID, &requests[i]); err != nil {
			return r.routePostSendingFailure(ctx, group.envelopes, err)
		}
	}
	return r.markDispatched(ctx, group.envelopes)
}
