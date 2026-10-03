package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/park285/iris-client-go/v3/iris"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
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
			return err
		}

		if done {
			return nil
		}

		releasable = resolved
	}

	if err := r.completeFailureFinalization(ctx, releasable, routeErr); err != nil {
		return err
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
			return nil, false, err
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

	// 명시적 unknown 증거는 함께 전달된 pre-handoff 오류보다 우선하며 재발급하지 않는다.
	if sendoutcome.Classify(cause) == sendoutcome.OutcomeUnknown {
		return r.quarantinePostSendingFailure(ctx, envelopes, cause)
	}

	if sendoutcome.Classify(cause) == sendoutcome.Failed && iris.IsPreHandoffClientRequestIDConflict(cause) && group.request != nil {
		return r.reissueFailedRequest(ctx, group, cause)
	}

	// 응답 유실은 저장된 최종 본문·경로·membership·ID를 그대로 재사용할 수 있을 때만 재시도한다.
	if isAlarmDispatchNotAdmittedRetryableFailure(cause) ||
		(group.request != nil && hasPersistedClientRequestID(envelopes) && isAlarmDispatchAmbiguousPostSendFailure(cause)) {
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

// 재시도 허가는 Text 경로가 실제로 보낸 ID(alarmDispatchClientRequestID)가 저장된 send-unit ID일 때만 참이다.
func hasPersistedClientRequestID(envelopes []domain.AlarmQueueEnvelope) bool {
	return persistedAlarmDispatchClientRequestIDFromEnvelopes(envelopes) != ""
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

// Transport ambiguity는 고정된 request를 재사용할 때만 허용되며 횟수 예산은 현행 값을 따른다.
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
	return !errors.Is(cause, context.Canceled) && sendoutcome.Classify(cause) == sendoutcome.TransportAmbiguous
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

func (r *Runner) reissueFailedRequest(ctx context.Context, group alarmDispatchGroup, cause error) error {
	retry, terminal := prepareDispatchFailure(group.envelopes, cause)
	if len(terminal) == 0 {
		reissued, err := r.consumer.ReissueSendRequest(ctx, retry, group.request.ClientRequestID)
		if err != nil {
			return fmt.Errorf("persist reissued alarm request: %w", err)
		}

		if reissued {
			return nil
		}
	}

	return r.quarantinePostSendingFailure(ctx, group.envelopes, fmt.Errorf("alarm request reissue budget exhausted: %w", cause))
}
