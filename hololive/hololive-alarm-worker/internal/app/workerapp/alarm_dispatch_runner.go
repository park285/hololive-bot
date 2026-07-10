package workerapp

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

type alarmDispatchConsumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	ReleaseClaimKeys(ctx context.Context, claimKeys []string) error
	ScheduleRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MoveToDLQ(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
}

type alarmDispatchQuarantineConsumer interface {
	Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, reason string) error
}

type alarmDispatchSendingRetryConsumer interface {
	ScheduleSendingRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
}

type alarmDispatchIdleWaiter interface {
	Wait(ctx context.Context) bool
	Reset()
}

type alarmDispatchSender interface {
	SendMessage(ctx context.Context, roomID, message string) error
	SendKaringContentList(ctx context.Context, roomID string, req *iris.KaringContentListRequest) error
}

type alarmDispatchClientRequestSender interface {
	SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error
}

type alarmDispatchRunner struct {
	consumer           alarmDispatchConsumer
	sender             alarmDispatchSender
	renderer           *template.Renderer
	messageStrings     *messagestrings.Store
	idleWaiter         alarmDispatchIdleWaiter
	karingEnabled      bool
	consumerMode       string
	postSendQuarantine bool
	maxBatch           int
	maxBatchesPerWake  int
	batchesSinceWake   int
	yield              func(context.Context) bool
	logger             *slog.Logger
}

func (r *alarmDispatchRunner) runOnce(ctx context.Context) (bool, error) {
	envelopes, err := r.consumer.DrainBatch(ctx, r.maxBatch)
	if err != nil {
		return false, fmt.Errorf("drain alarm dispatch batch: %w", err)
	}
	if len(envelopes) == 0 {
		return false, nil
	}
	return true, r.dispatchGroups(ctx, groupAlarmDispatchEnvelopesForKaring(envelopes, r.karingEnabled))
}

func (r *alarmDispatchRunner) dispatchGroups(ctx context.Context, groups []alarmDispatchGroup) error {
	for _, group := range groups {
		if err := r.dispatchGroup(ctx, group); err != nil {
			return err
		}
	}
	return nil
}

func (r *alarmDispatchRunner) dispatchGroup(ctx context.Context, group alarmDispatchGroup) error {
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

func (r *alarmDispatchRunner) dispatchMessageGroup(ctx context.Context, group alarmDispatchGroup) error {
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

func sendAlarmDispatchMessage(ctx context.Context, sender alarmDispatchSender, group alarmDispatchGroup, message string) error {
	if clientRequestSender, ok := sender.(alarmDispatchClientRequestSender); ok {
		return clientRequestSender.SendMessageWithClientRequestID(ctx, group.roomID, message, alarmDispatchClientRequestID(group, 0, len(group.envelopes)))
	}
	return sender.SendMessage(ctx, group.roomID, message)
}

func (r *alarmDispatchRunner) dispatchKaringContentListGroup(ctx context.Context, group alarmDispatchGroup) error {
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

func (r *alarmDispatchRunner) persistPreSendFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	retryEnvelopes, dlqEnvelopes := prepareDispatchFailure(envelopes, cause)
	return r.finalizeDispatchFailure(ctx, retryEnvelopes, dlqEnvelopes, func(scheduleEnvelopes []domain.AlarmQueueEnvelope) error {
		if err := r.consumer.ScheduleRetry(ctx, scheduleEnvelopes); err != nil {
			return fmt.Errorf("schedule alarm dispatch retry: %w", err)
		}
		return nil
	})
}

func (r *alarmDispatchRunner) finalizeDispatchFailure(
	ctx context.Context,
	retryEnvelopes []domain.AlarmQueueEnvelope,
	dlqEnvelopes []domain.AlarmQueueEnvelope,
	scheduleFn func(envelopes []domain.AlarmQueueEnvelope) error,
) error {
	if err := scheduleFn(retryEnvelopes); err != nil {
		return r.preserveAfterPersistenceFailure(ctx, retryEnvelopes, err)
	}
	if err := r.consumer.MoveToDLQ(ctx, dlqEnvelopes); err != nil {
		return r.preserveAfterPersistenceFailure(ctx, dlqEnvelopes, fmt.Errorf("move alarm dispatch dlq: %w", err))
	}
	if err := r.consumer.ReleaseClaimKeys(ctx, claimKeysForAlarmDispatchEnvelopes(dlqEnvelopes)); err != nil {
		return fmt.Errorf("release alarm dispatch dlq claim keys: %w", err)
	}
	return nil
}

// MarkSending 에러 시 UPDATE는 이미 커밋된 뒤라 'sending' 잔류 행은 leased 전용 ScheduleRetry로
// 복원 불가 — status IN ('leased','sending')을 덮는 ScheduleSendingRetry로 보상한다(발송 전이라 중복 없음).
func (r *alarmDispatchRunner) persistMarkSendingFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	if _, ok := r.consumer.(alarmDispatchSendingRetryConsumer); !ok {
		return fmt.Errorf("mark alarm dispatch sending: %w", cause)
	}
	return r.persistSendingRetry(ctx, envelopes, cause)
}

func (r *alarmDispatchRunner) persistPostSendingFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
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

func (r *alarmDispatchRunner) persistSendingRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	consumer, ok := r.consumer.(alarmDispatchSendingRetryConsumer)
	if !ok {
		return r.persistPreSendFailure(ctx, envelopes, cause)
	}
	retryEnvelopes, dlqEnvelopes := prepareDispatchFailure(envelopes, cause)
	return r.finalizeDispatchFailure(ctx, retryEnvelopes, dlqEnvelopes, func(scheduleEnvelopes []domain.AlarmQueueEnvelope) error {
		if err := consumer.ScheduleSendingRetry(ctx, scheduleEnvelopes); err != nil {
			return fmt.Errorf("schedule alarm dispatch sending retry: %w", err)
		}
		return nil
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

func (r *alarmDispatchRunner) preserveAfterPersistenceFailure(
	ctx context.Context,
	envelopes []domain.AlarmQueueEnvelope,
	persistErr error,
) error {
	if len(envelopes) == 0 {
		return persistErr
	}
	if err := r.consumer.Requeue(ctx, envelopes); err != nil {
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
