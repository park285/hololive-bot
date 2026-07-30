package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
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
	Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
}

type alarmDispatchQuarantineConsumer interface {
	Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, reason string) error
}

type alarmDispatchSendingFailureConsumer interface {
	RouteSendingFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error
}

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
	consumer           Consumer
	sender             Sender
	renderer           *template.Renderer
	messageStrings     *messagestrings.Store
	idleWaiter         IdleWaiter
	karingEnabled      bool
	consumerMode       string
	postSendQuarantine bool
	maxBatch           int
	maxBatchesPerWake  int
	batchesSinceWake   int
	yield              func(context.Context) bool
	logger             *slog.Logger
}

type RunnerConfig struct {
	KaringEnabled      bool
	ConsumerMode       string
	PostSendQuarantine bool
	MaxBatch           int
	MaxBatchesPerWake  int
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
		consumer:           consumer,
		sender:             sender,
		renderer:           renderer,
		messageStrings:     messageStrings,
		idleWaiter:         idleWaiter,
		karingEnabled:      config.KaringEnabled,
		consumerMode:       config.ConsumerMode,
		postSendQuarantine: config.PostSendQuarantine,
		maxBatch:           config.MaxBatch,
		maxBatchesPerWake:  config.MaxBatchesPerWake,
		logger:             logger,
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

func (r *Runner) persistPreSendFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	retryEnvelopes, dlqEnvelopes := prepareDispatchFailure(envelopes, cause)
	return r.finalizeDispatchFailure(ctx, retryEnvelopes, dlqEnvelopes, func(retry, dlq []domain.AlarmQueueEnvelope) error {
		if err := r.consumer.RouteFailures(ctx, retry, dlq); err != nil {
			return fmt.Errorf("route alarm dispatch failure: %w", err)
		}
		return nil
	}, r.consumer.Requeue)
}

func (r *Runner) finalizeDispatchFailure(
	ctx context.Context,
	retryEnvelopes []domain.AlarmQueueEnvelope,
	dlqEnvelopes []domain.AlarmQueueEnvelope,
	routeFn func(retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error,
	requeueFn func(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error,
) error {
	routeErr := routeFn(retryEnvelopes, dlqEnvelopes)
	releasable := dlqEnvelopes
	if routeErr != nil {
		unapplied, partial := unappliedFailureRoutingIDs(routeErr)
		if !partial {
			return r.preserveAfterPersistenceFailure(ctx, combineEnvelopes(retryEnvelopes, dlqEnvelopes), requeueFn, routeErr)
		}
		// 부분 적용의 미적용 행은 recovery/quarantine 경로 소유 — requeue로 덮어쓰지 않고,
		// claim key는 실제 dlq로 전이된 부분집합만 해제한다.
		releasable = envelopesExcludingIDs(dlqEnvelopes, unapplied)
	}
	if err := r.consumer.ReleaseClaimKeys(ctx, claimKeysForAlarmDispatchEnvelopes(releasable)); err != nil {
		if routeErr != nil {
			return fmt.Errorf("%w: release alarm dispatch dlq claim keys: %w", routeErr, err)
		}
		return fmt.Errorf("release alarm dispatch dlq claim keys: %w", err)
	}
	return routeErr
}

// MarkSending 에러 시 UPDATE는 이미 커밋된 뒤라 'sending' 잔류 행은 leased 전용 RouteFailures로
// 복원 불가 — status IN ('leased','sending')을 덮는 RouteSendingFailures로 보상한다(발송 전이라 중복 없음).
func (r *Runner) persistMarkSendingFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	if _, ok := r.consumer.(alarmDispatchSendingFailureConsumer); !ok {
		return fmt.Errorf("mark alarm dispatch sending: %w", cause)
	}
	return r.persistSendingRetry(ctx, envelopes, cause)
}

func (r *Runner) persistPostSendingFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	if isAlarmDispatchRetryablePostSendFailure(cause) {
		return r.persistSendingRetry(ctx, envelopes, cause)
	}
	if !r.postSendQuarantine {
		return r.persistPreSendFailure(ctx, envelopes, cause)
	}
	consumer, ok := r.consumer.(alarmDispatchQuarantineConsumer)
	if !ok {
		return r.persistPreSendFailure(ctx, envelopes, cause)
	}
	reason := cause.Error()
	if err := consumer.Quarantine(ctx, envelopes, reason); err != nil {
		return fmt.Errorf("quarantine alarm dispatch after send failure: %w", err)
	}
	observeAlarmDispatchRunnerPostSendQuarantined(len(envelopes))
	return nil
}

func (r *Runner) persistSendingRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	consumer, ok := r.consumer.(alarmDispatchSendingFailureConsumer)
	if !ok {
		return r.persistPreSendFailure(ctx, envelopes, cause)
	}
	retryEnvelopes, dlqEnvelopes := prepareDispatchFailure(envelopes, cause)
	return r.finalizeDispatchFailure(ctx, retryEnvelopes, dlqEnvelopes, func(retry, dlq []domain.AlarmQueueEnvelope) error {
		if err := consumer.RouteSendingFailures(ctx, retry, dlq); err != nil {
			return fmt.Errorf("route alarm dispatch sending failure: %w", err)
		}
		return nil
	}, func(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
		// 'sending' 잔류 행은 leased 전용 Requeue(RouteFailures fence)에 매칭되지 않아
		// 일시적 infra 오류가 QuarantineStaleSending의 terminal quarantine으로 굳는다.
		// fallback도 sending fence로 전량 retry 복원한다.
		return consumer.RouteSendingFailures(ctx, envelopes, nil)
	})
}

func isAlarmDispatchRetryablePostSendFailure(cause error) bool {
	if cause == nil {
		return false
	}
	var httpErr *iris.HTTPError
	if errors.As(cause, &httpErr) {
		return httpErr.StatusCode == 429 || httpErr.StatusCode == 502 || httpErr.StatusCode == 503
	}
	return false
}

func (r *Runner) preserveAfterPersistenceFailure(
	ctx context.Context,
	envelopes []domain.AlarmQueueEnvelope,
	requeueFn func(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error,
	persistErr error,
) error {
	if len(envelopes) == 0 {
		return persistErr
	}
	if err := requeueFn(ctx, envelopes); err != nil {
		return fmt.Errorf("%w: fallback requeue: %w", persistErr, err)
	}
	return persistErr
}

func claimKeysForAlarmDispatchEnvelopes(envelopes []domain.AlarmQueueEnvelope) []string {
	claimKeys := make([]string, 0, len(envelopes))
	for i := range envelopes {
		claimKeys = append(claimKeys, envelopes[i].ClaimKeys...)
	}
	return claimKeys
}

type partialFailureRouting interface {
	error
	UnappliedDeliveryIDs() []int64
}

func unappliedFailureRoutingIDs(err error) ([]int64, bool) {
	var partial partialFailureRouting
	if !errors.As(err, &partial) {
		return nil, false
	}
	return partial.UnappliedDeliveryIDs(), true
}

func combineEnvelopes(a, b []domain.AlarmQueueEnvelope) []domain.AlarmQueueEnvelope {
	combined := make([]domain.AlarmQueueEnvelope, 0, len(a)+len(b))
	combined = append(combined, a...)
	return append(combined, b...)
}

func envelopesExcludingIDs(envelopes []domain.AlarmQueueEnvelope, ids []int64) []domain.AlarmQueueEnvelope {
	if len(ids) == 0 {
		return envelopes
	}
	excluded := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		excluded[id] = struct{}{}
	}
	kept := make([]domain.AlarmQueueEnvelope, 0, len(envelopes))
	for i := range envelopes {
		if _, ok := excluded[envelopes[i].DispatchOutboxID]; ok {
			continue
		}
		kept = append(kept, envelopes[i])
	}
	return kept
}

func prepareDispatchFailure(envelopes []domain.AlarmQueueEnvelope, cause error) (retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) {
	retryEnvelopes = make([]domain.AlarmQueueEnvelope, 0, len(envelopes))
	dlqEnvelopes = make([]domain.AlarmQueueEnvelope, 0, len(envelopes))
	for i := range envelopes {
		updated := envelopes[i]
		updated.Retry = nextAlarmDispatchRetry(&envelopes[i], cause)
		if updated.Retry.Attempt >= 3 {
			dlqEnvelopes = append(dlqEnvelopes, updated)
			continue
		}
		retryEnvelopes = append(retryEnvelopes, updated)
	}
	return retryEnvelopes, dlqEnvelopes
}

const maxHTTPRetryAfter = 5 * time.Minute

func nextAlarmDispatchRetry(envelope *domain.AlarmQueueEnvelope, cause error) *domain.AlarmQueueRetryMetadata {
	retry := &domain.AlarmQueueRetryMetadata{}
	if envelope.Retry != nil {
		*retry = *envelope.Retry
	}
	retry.Attempt++
	retry.LastError = cause.Error()
	retryAfter := time.Duration(retry.Attempt) * 5 * time.Second
	var httpErr *iris.HTTPError
	if errors.As(cause, &httpErr) && httpErr.RetryAfter > retryAfter {
		hint := httpErr.RetryAfter
		if hint > maxHTTPRetryAfter {
			hint = maxHTTPRetryAfter
			observeAlarmDispatchRetryAfterClamped()
		}
		retryAfter = hint
	}
	retry.RetryAfterMS = int64(retryAfter / time.Millisecond)
	retry.NextVisibleAt = time.Now().UTC().Add(retryAfter).Format(time.RFC3339Nano)
	return retry
}
