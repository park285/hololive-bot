package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
)

func (r *Runner) persistPreSendFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	retryEnvelopes, dlqEnvelopes := prepareDispatchFailure(envelopes, cause)

	if err := r.finalizeDispatchFailure(ctx, retryEnvelopes, dlqEnvelopes, func(retry, dlq []domain.AlarmQueueEnvelope) error {
		if err := r.consumer.RouteFailures(ctx, retry, dlq); err != nil {
			return fmt.Errorf("route alarm dispatch before send failure: %w", err)
		}

		return nil
	}, r.consumer.Requeue); err != nil {
		return err
	}

	return nil
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
		resolved, done, err := r.resolveFailureRoute(ctx, retryEnvelopes, dlqEnvelopes, requeueFn, routeErr)
		if err != nil {
			return errors.Join(err)
		}

		if done {
			return nil
		}

		releasable = resolved
	}

	if err := r.completeFailureFinalization(ctx, releasable, routeErr); err != nil {
		return errors.Join(err)
	}

	return nil
}

func (r *Runner) resolveFailureRoute(
	ctx context.Context,
	retryEnvelopes []domain.AlarmQueueEnvelope,
	dlqEnvelopes []domain.AlarmQueueEnvelope,
	requeueFn func(context.Context, []domain.AlarmQueueEnvelope) error,
	routeErr error,
) ([]domain.AlarmQueueEnvelope, bool, error) {
	unapplied, partial := unappliedFailureRoutingIDs(routeErr)
	if !partial {
		if err := r.preserveAfterPersistenceFailure(ctx, combineEnvelopes(retryEnvelopes, dlqEnvelopes), requeueFn, routeErr); err != nil {
			return nil, false, errors.Join(err)
		}

		return nil, true, nil
	}

	return envelopesExcludingIDs(dlqEnvelopes, unapplied), false, nil
}

func (r *Runner) completeFailureFinalization(ctx context.Context, releasable []domain.AlarmQueueEnvelope, routeErr error) error {
	if err := r.consumer.ReleaseClaimKeys(ctx, claimKeysForAlarmDispatchEnvelopes(releasable)); err != nil {
		if routeErr != nil {
			return fmt.Errorf("%w: release alarm dispatch dlq claim keys: %w", routeErr, err)
		}

		return fmt.Errorf("release alarm dispatch dlq claim keys: %w", err)
	}

	if routeErr != nil {
		return fmt.Errorf("route alarm dispatch failure: %w", routeErr)
	}

	return nil
}

func (r *Runner) persistMarkSendingFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	requeued := preparePreSendRequeue(envelopes, cause)
	if err := r.consumer.RequeuePreSend(ctx, requeued); err != nil {
		return fmt.Errorf("requeue alarm dispatch before send: %w", err)
	}

	return nil
}

func (r *Runner) persistPostSendingFailure(ctx context.Context, group alarmDispatchGroup, cause error) error {
	envelopes := group.envelopes

	if errors.Is(cause, egress.ErrKaringOutcomeUnknown) {
		return r.quarantinePostSendingFailure(ctx, envelopes, cause)
	}

	// Room facts는 다음 drain에서 바뀔 수 있고 선택된 path는 영속되지 않는다. ambiguous 발송을
	// 재드레인하면 text와 Karing 사이에서 payload와 ClientRequestID가 함께 바뀔 수 있다.
	if group.egressRoomScoped && isAlarmDispatchAmbiguousPostSendFailure(cause) {
		return r.quarantinePostSendingFailure(ctx, envelopes, cause)
	}

	if alarmDispatchPostSendFailureIsRetryable(cause, len(envelopes)) ||
		(hasPersistedClientRequestID(envelopes) && isAlarmDispatchAmbiguousPostSendFailure(cause)) {
		if err := r.persistSendingRetry(ctx, envelopes, cause); err != nil {
			return fmt.Errorf("persist sending retry: %w", err)
		}

		return nil
	}

	return r.quarantinePostSendingFailure(ctx, envelopes, cause)
}

func (r *Runner) quarantinePostSendingFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	if err := r.consumer.Quarantine(ctx, envelopes, cause); err != nil {
		return fmt.Errorf("quarantine alarm dispatch after send failure: %w", err)
	}

	observeAlarmDispatchRunnerPostSendQuarantined(len(envelopes))

	return nil
}

func hasPersistedClientRequestID(envelopes []domain.AlarmQueueEnvelope) bool {
	return persistedAlarmDispatchClientRequestID(alarmDispatchGroup{envelopes: envelopes}) != ""
}

func (r *Runner) persistSendingRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	retryEnvelopes, dlqEnvelopes := prepareDispatchFailure(envelopes, cause)

	if err := r.finalizeDispatchFailure(ctx, retryEnvelopes, dlqEnvelopes, func(retry, dlq []domain.AlarmQueueEnvelope) error {
		if err := r.consumer.RouteSendingFailures(ctx, retry, dlq); err != nil {
			return fmt.Errorf("route alarm dispatch sending failure: %w", err)
		}

		return nil
	}, func(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
		// 'sending' 잔류 행은 leased 전용 Requeue(RouteFailures fence)에 매칭되지 않아
		// 일시적 infra 오류가 QuarantineStaleSending의 terminal quarantine으로 굳는다.
		// fallback도 sending fence로 전량 retry 복원한다.
		return r.consumer.RouteSendingFailures(ctx, envelopes, nil)
	}); err != nil {
		return err
	}

	return nil
}

// TransportError/DeadlineExceeded는 응답을 한 번도 받지 못한 경우라 첫 발송이 이미
// admission됐을 수 있다. Room-scoped path는 호출 전에 quarantine하고, 남은 고정 path에서도
// ambiguous 원인은 solo 재그룹핑으로 ID가 그대로 재생산되는 단건 그룹에만 재시도를 허용한다.
// 429/502/503은 미수용이 확정된 응답이라 그룹 크기와 무관하게 재시도한다.
func alarmDispatchPostSendFailureIsRetryable(cause error, envelopeCount int) bool {
	if isAlarmDispatchNotAdmittedRetryableFailure(cause) {
		return true
	}

	return envelopeCount == 1 && isAlarmDispatchAmbiguousPostSendFailure(cause)
}

func isAlarmDispatchRetryablePostSendFailure(cause error) bool {
	return isAlarmDispatchNotAdmittedRetryableFailure(cause) || isAlarmDispatchAmbiguousPostSendFailure(cause)
}

func isAlarmDispatchNotAdmittedRetryableFailure(cause error) bool {
	if httpErr, ok := errors.AsType[*iris.HTTPError](cause); ok {
		return httpErr.StatusCode == http.StatusTooManyRequests || httpErr.StatusCode == http.StatusBadGateway || httpErr.StatusCode == http.StatusServiceUnavailable
	}

	return false
}

func isAlarmDispatchAmbiguousPostSendFailure(cause error) bool {
	if cause == nil {
		return false
	}

	if errors.Is(cause, egress.ErrKaringOutcomeUnknown) {
		return false
	}

	if _, ok := errors.AsType[*iris.TransportError](cause); ok {
		return true
	}

	return errors.Is(cause, context.DeadlineExceeded)
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
	partial, ok := errors.AsType[partialFailureRouting](err)
	if !ok {
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

const (
	alarmDispatchMaxAttempts          = 3
	alarmDispatchRetryableMaxAttempts = 6
)

// attempt*5s 선형 백오프에서 3회는 누적 15s라 Iris 재기동(30~60s)을 넘기지 못하고
// 대기 물량 전체를 DLQ로 흘린다. 일시적 원인만 6회(누적 75s)로 늘린다.
func alarmDispatchMaxAttemptsForCause(cause error) int {
	if isAlarmDispatchRetryablePostSendFailure(cause) {
		return alarmDispatchRetryableMaxAttempts
	}

	return alarmDispatchMaxAttempts
}

func prepareDispatchFailure(envelopes []domain.AlarmQueueEnvelope, cause error) (retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) {
	retryEnvelopes = make([]domain.AlarmQueueEnvelope, 0, len(envelopes))
	dlqEnvelopes = make([]domain.AlarmQueueEnvelope, 0, len(envelopes))

	maxAttempts := alarmDispatchMaxAttemptsForCause(cause)

	for i := range envelopes {
		updated := envelopes[i]

		updated.Retry = nextAlarmDispatchRetry(&envelopes[i], cause)

		if updated.Retry.Attempt >= maxAttempts {
			dlqEnvelopes = append(dlqEnvelopes, updated)
			continue
		}

		retryEnvelopes = append(retryEnvelopes, updated)
	}

	return retryEnvelopes, dlqEnvelopes
}

func preparePreSendRequeue(envelopes []domain.AlarmQueueEnvelope, cause error) []domain.AlarmQueueEnvelope {
	requeued := make([]domain.AlarmQueueEnvelope, 0, len(envelopes))
	for i := range envelopes {
		updated := envelopes[i]

		updated.Retry = sameAttemptAlarmDispatchRetry(&envelopes[i], cause)
		requeued = append(requeued, updated)
	}

	return requeued
}

func sameAttemptAlarmDispatchRetry(envelope *domain.AlarmQueueEnvelope, cause error) *domain.AlarmQueueRetryMetadata {
	retry := &domain.AlarmQueueRetryMetadata{}

	if envelope.Retry != nil {
		*retry = *envelope.Retry
	}

	retry.LastError = cause.Error()
	retry.LastErrorCode = dispatchoutbox.ClassifyErrorCode(cause)
	retry.RetryAfterMS = int64((5 * time.Second) / time.Millisecond)
	retry.NextVisibleAt = time.Now().UTC().Add(5 * time.Second).Format(time.RFC3339Nano)

	return retry
}

const maxHTTPRetryAfter = 5 * time.Minute

func nextAlarmDispatchRetry(envelope *domain.AlarmQueueEnvelope, cause error) *domain.AlarmQueueRetryMetadata {
	retry := &domain.AlarmQueueRetryMetadata{}

	if envelope.Retry != nil {
		*retry = *envelope.Retry
	}

	retry.Attempt++

	retry.LastError = cause.Error()
	retry.LastErrorCode = dispatchoutbox.ClassifyErrorCode(cause)

	retryAfter := time.Duration(retry.Attempt) * 5 * time.Second

	if httpErr, ok := errors.AsType[*iris.HTTPError](cause); ok && httpErr.RetryAfter > retryAfter {
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
