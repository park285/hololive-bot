package dispatchrun

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/iris-client-go/iris"
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
}

type RunnerConfig struct {
	KaringEnabled     bool
	MaxBatch          int
	MaxBatchesPerWake int
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
	return true, r.dispatchGroups(ctx, groupAlarmDispatchEnvelopesForKaring(envelopes, r.karingEnabled))
}

func (r *Runner) dispatchGroups(ctx context.Context, groups []alarmDispatchGroup) error {
	for _, group := range groups {
		if err := r.dispatchGroup(ctx, group); err != nil {
			return err
		}
	}
	return nil
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
	message, err := renderAlarmDispatchGroup(ctx, r.renderer, r.messageStrings, group)
	if err != nil {
		return r.persistPreSendFailure(ctx, group.envelopes, err)
	}
	if err := r.consumer.MarkSending(ctx, group.envelopes); err != nil {
		return r.persistMarkSendingFailure(ctx, group.envelopes, err)
	}
	if err := sendAlarmDispatchMessage(ctx, r.sender, group, message); err != nil {
		return r.persistPostSendingFailure(ctx, group.envelopes, err)
	}
	if err := r.consumer.MarkDispatched(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark alarm dispatch sent: %w", err)
	}
	return nil
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
		return r.persistPreSendFailure(ctx, group.envelopes, err)
	}
	if err := r.consumer.MarkSending(ctx, group.envelopes); err != nil {
		return r.persistMarkSendingFailure(ctx, group.envelopes, err)
	}
	for i := range requests {
		if err := r.sender.SendKaringContentList(ctx, group.roomID, &requests[i]); err != nil {
			return r.persistPostSendingFailure(ctx, group.envelopes, err)
		}
	}
	if err := r.consumer.MarkDispatched(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark alarm dispatch sent: %w", err)
	}
	return nil
}
