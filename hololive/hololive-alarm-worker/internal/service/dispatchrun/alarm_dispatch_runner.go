package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
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
	shortLinkBaseURL  string
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

func (r *Runner) routePostSendingFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	if err := r.withStateContext(ctx, func(stateCtx context.Context) error {
		return r.persistPostSendingFailure(stateCtx, envelopes, cause)
	}); err != nil {
		return fmt.Errorf("with state context: %w", err)
	}

	return nil
}

func (r *Runner) dispatchGroup(ctx context.Context, group alarmDispatchGroup) error {
	if alarmDispatchGroupUsesTextPath(group) {
		if err := r.dispatchMessageGroup(ctx, group); err != nil {
			return fmt.Errorf("dispatch message group: %w", err)
		}

		return nil
	}

	if !r.karingEnabled {
		if err := r.dispatchMessageGroup(ctx, group); err != nil {
			return fmt.Errorf("dispatch message group: %w", err)
		}

		return nil
	}

	if err := r.dispatchKaringContentListGroup(ctx, group); err != nil {
		return fmt.Errorf("dispatch karing content list group: %w", err)
	}

	return nil
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
	message, err := renderAlarmDispatchGroup(ctx, r.renderer, r.messageStrings, r.members, r.shortLinkBaseURL, group)
	if err != nil {
		if routeErr := r.routePreSendFailure(ctx, group.envelopes, err); routeErr != nil {
			return fmt.Errorf("route pre send failure: %w", routeErr)
		}

		return nil
	}

	if err := r.dispatchRenderedMessageGroup(ctx, group, message); err != nil {
		return fmt.Errorf("dispatch rendered message group: %w", err)
	}

	return nil
}

func (r *Runner) dispatchRenderedMessageGroup(ctx context.Context, group alarmDispatchGroup, message string) error {
	// markSending은 실패를 영속화까지 마치면 err 없이 proceed=false를 돌려준다. nil을 감싸면
	// 정상적인 발송 중단이 루프 오류로 바뀐다.
	if proceed, markErr := r.markSending(ctx, group.envelopes); !proceed {
		if markErr != nil {
			return fmt.Errorf("mark alarm dispatch sending: %w", markErr)
		}

		return nil
	}

	if sendErr := sendAlarmDispatchMessage(ctx, r.sender, group, message); sendErr != nil {
		if routeErr := r.routePostSendingFailure(ctx, group.envelopes, sendErr); routeErr != nil {
			return fmt.Errorf("route post sending failure: %w", routeErr)
		}

		return nil
	}

	if err := r.markDispatched(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark dispatched: %w", err)
	}

	return nil
}

func sendAlarmDispatchMessage(ctx context.Context, sender Sender, group alarmDispatchGroup, message string) error {
	if idSender, ok := sender.(clientRequestSender); ok {
		if err := idSender.SendMessageWithClientRequestID(ctx, group.roomID, message, alarmDispatchClientRequestID(group, 0, len(group.envelopes))); err != nil {
			return fmt.Errorf("send message with client request ID: %w", err)
		}

		return nil
	}

	if err := sender.SendMessage(ctx, group.roomID, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (r *Runner) dispatchKaringContentListGroup(ctx context.Context, group alarmDispatchGroup) error {
	requests, err := buildAlarmDispatchKaringContentListRequests(ctx, r.messageStrings, group)
	if err != nil {
		if routeErr := r.routePreSendFailure(ctx, group.envelopes, err); routeErr != nil {
			return fmt.Errorf("route pre send failure: %w", routeErr)
		}

		return nil
	}

	if err := r.dispatchKaringRequests(ctx, group, requests); err != nil {
		return fmt.Errorf("dispatch karing requests: %w", err)
	}

	return nil
}

func (r *Runner) dispatchKaringRequests(ctx context.Context, group alarmDispatchGroup, requests []iris.KaringContentListRequest) error {
	// markSending은 실패를 영속화까지 마치면 err 없이 proceed=false를 돌려준다. nil을 감싸면
	// 정상적인 발송 중단이 루프 오류로 바뀐다.
	if proceed, markErr := r.markSending(ctx, group.envelopes); !proceed {
		if markErr != nil {
			return fmt.Errorf("mark alarm dispatch sending: %w", markErr)
		}

		return nil
	}

	sent, err := r.sendKaringRequests(ctx, group, requests)
	if err != nil {
		return fmt.Errorf("send karing requests: %w", err)
	}

	if !sent {
		return nil
	}

	if err := r.markDispatched(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark dispatched: %w", err)
	}

	return nil
}

func (r *Runner) sendKaringRequests(ctx context.Context, group alarmDispatchGroup, requests []iris.KaringContentListRequest) (bool, error) {
	for i := range requests {
		if sendErr := r.sender.SendKaringContentList(ctx, group.roomID, &requests[i]); sendErr != nil {
			if routeErr := r.routePostSendingFailure(ctx, group.envelopes, sendErr); routeErr != nil {
				return false, fmt.Errorf("route post sending failure: %w", routeErr)
			}

			return false, nil
		}
	}

	return true, nil
}
