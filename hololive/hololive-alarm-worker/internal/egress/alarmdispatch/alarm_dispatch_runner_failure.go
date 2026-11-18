package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/park285/iris-client-go/v5/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

func (r *Runner) persistPreSendFailure(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) error {
	retryEnvelopes, dlqEnvelopes := prepareDispatchFailure(envelopes, cause)

	if err := r.finalizeDispatchFailure(ctx, retryEnvelopes, dlqEnvelopes, func(retry, dlq []dispatchoutbox.AlarmQueueEnvelope) error {
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
	retryEnvelopes []dispatchoutbox.AlarmQueueEnvelope,
	dlqEnvelopes []dispatchoutbox.AlarmQueueEnvelope,
	routeFn func(retryEnvelopes, dlqEnvelopes []dispatchoutbox.AlarmQueueEnvelope) error,
	requeueFn func(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error,
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
	retryEnvelopes []dispatchoutbox.AlarmQueueEnvelope,
	dlqEnvelopes []dispatchoutbox.AlarmQueueEnvelope,
	requeueFn func(context.Context, []dispatchoutbox.AlarmQueueEnvelope) error,
	routeErr error,
) ([]dispatchoutbox.AlarmQueueEnvelope, bool, error) {
	unapplied, partial := unappliedFailureRoutingIDs(routeErr)
	if !partial {
		if err := r.preserveAfterPersistenceFailure(ctx, combineEnvelopes(retryEnvelopes, dlqEnvelopes), requeueFn, routeErr); err != nil {
			return nil, false, err
		}

		return nil, true, nil
	}

	return envelopesExcludingIDs(dlqEnvelopes, unapplied), false, nil
}

func (r *Runner) completeFailureFinalization(ctx context.Context, releasable []dispatchoutbox.AlarmQueueEnvelope, routeErr error) error {
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

func (r *Runner) persistMarkSendingFailure(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) error {
	requeued := preparePreSendRequeue(envelopes, cause)
	if err := r.consumer.RequeuePreSend(ctx, requeued); err != nil {
		return fmt.Errorf("requeue alarm dispatch before send: %w", err)
	}

	return nil
}

func (r *Runner) persistPostSendingFailure(ctx context.Context, group alarmDispatchGroup, cause error) error {
	envelopes := group.envelopes

	if sendoutcome.Classify(cause) == sendoutcome.OutcomeUnknown {
		return r.quarantinePostSendingFailure(ctx, envelopes, cause)
	}

	if sendoutcome.Reissuable(cause) && group.request != nil {
		return r.reissueFailedRequest(ctx, group, cause)
	}

	if isAlarmDispatchNotAdmittedRetryableFailure(cause) ||
		(group.request != nil && isAlarmDispatchAmbiguousPostSendFailure(cause)) {
		if err := r.persistSendingRetry(ctx, envelopes, cause); err != nil {
			return fmt.Errorf("persist sending retry: %w", err)
		}

		return nil
	}

	return r.quarantinePostSendingFailure(ctx, envelopes, cause)
}

func (r *Runner) quarantinePostSendingFailure(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) error {
	if err := r.consumer.Quarantine(ctx, envelopes, cause); err != nil {
		return fmt.Errorf("quarantine alarm dispatch after send failure: %w", err)
	}

	observeAlarmDispatchRunnerPostSendQuarantined(len(envelopes))

	return nil
}

func (r *Runner) persistSendingRetry(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) error {
	retryEnvelopes, dlqEnvelopes := prepareDispatchFailure(envelopes, cause)

	if err := r.finalizeDispatchFailure(ctx, retryEnvelopes, dlqEnvelopes, func(retry, dlq []dispatchoutbox.AlarmQueueEnvelope) error {
		if err := r.consumer.RouteSendingFailures(ctx, retry, dlq); err != nil {
			return fmt.Errorf("route alarm dispatch sending failure: %w", err)
		}

		return nil
	}, func(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error {
		return r.consumer.RouteSendingFailures(ctx, envelopes, nil)
	}); err != nil {
		return err
	}

	return nil
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
	return !errors.Is(cause, context.Canceled) && sendoutcome.Classify(cause) == sendoutcome.TransportAmbiguous
}

func (r *Runner) preserveAfterPersistenceFailure(
	ctx context.Context,
	envelopes []dispatchoutbox.AlarmQueueEnvelope,
	requeueFn func(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error,
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

func claimKeysForAlarmDispatchEnvelopes(envelopes []dispatchoutbox.AlarmQueueEnvelope) []string {
	claimKeys := make([]string, 0, len(envelopes))
	for i := range envelopes {
		claimKeys = append(claimKeys, envelopes[i].ClaimKeys...)
	}

	return claimKeys
}

func unappliedFailureRoutingIDs(err error) ([]int64, bool) {
	miss, ok := errors.AsType[*sendfence.MissError](err)
	if !ok {
		return nil, false
	}

	return miss.UnappliedDeliveryIDs(), true
}

func combineEnvelopes(a, b []dispatchoutbox.AlarmQueueEnvelope) []dispatchoutbox.AlarmQueueEnvelope {
	combined := make([]dispatchoutbox.AlarmQueueEnvelope, 0, len(a)+len(b))

	combined = append(combined, a...)

	return append(combined, b...)
}

func envelopesExcludingIDs(envelopes []dispatchoutbox.AlarmQueueEnvelope, ids []int64) []dispatchoutbox.AlarmQueueEnvelope {
	if len(ids) == 0 {
		return envelopes
	}

	excluded := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		excluded[id] = struct{}{}
	}

	kept := make([]dispatchoutbox.AlarmQueueEnvelope, 0, len(envelopes))
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

func alarmDispatchMaxAttemptsForCause(cause error) int {
	if isAlarmDispatchRetryablePostSendFailure(cause) {
		return alarmDispatchRetryableMaxAttempts
	}

	return alarmDispatchMaxAttempts
}

func alarmDispatchRetryBackoff(attempt int) time.Duration {
	return time.Duration(attempt) * 5 * time.Second
}

func prepareDispatchFailure(envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) (retryEnvelopes, dlqEnvelopes []dispatchoutbox.AlarmQueueEnvelope) {
	retryEnvelopes = make([]dispatchoutbox.AlarmQueueEnvelope, 0, len(envelopes))
	dlqEnvelopes = make([]dispatchoutbox.AlarmQueueEnvelope, 0, len(envelopes))

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

func preparePreSendRequeue(envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) []dispatchoutbox.AlarmQueueEnvelope {
	requeued := make([]dispatchoutbox.AlarmQueueEnvelope, 0, len(envelopes))
	for i := range envelopes {
		updated := envelopes[i]

		updated.Retry = sameAttemptAlarmDispatchRetry(&envelopes[i], cause)
		requeued = append(requeued, updated)
	}

	return requeued
}

func sameAttemptAlarmDispatchRetry(envelope *dispatchoutbox.AlarmQueueEnvelope, cause error) *dispatchoutbox.AlarmQueueRetryMetadata {
	retry := &dispatchoutbox.AlarmQueueRetryMetadata{}

	if envelope.Retry != nil {
		*retry = *envelope.Retry
	}

	retry.LastError = cause.Error()
	retry.LastErrorCode = dispatchoutbox.ClassifyErrorCode(cause)
	retry.RetryAfterMS = int64((5 * time.Second) / time.Millisecond)

	return retry
}

const maxHTTPRetryAfter = 5 * time.Minute

func nextAlarmDispatchRetry(envelope *dispatchoutbox.AlarmQueueEnvelope, cause error) *dispatchoutbox.AlarmQueueRetryMetadata {
	retry := &dispatchoutbox.AlarmQueueRetryMetadata{}

	if envelope.Retry != nil {
		*retry = *envelope.Retry
	}

	retry.Attempt++

	retry.LastError = cause.Error()
	retry.LastErrorCode = dispatchoutbox.ClassifyErrorCode(cause)

	retryAfter := alarmDispatchRetryBackoff(retry.Attempt)

	if httpErr, ok := errors.AsType[*iris.HTTPError](cause); ok && httpErr.RetryAfter > retryAfter {
		hint := httpErr.RetryAfter
		if hint > maxHTTPRetryAfter {
			hint = maxHTTPRetryAfter

			observeAlarmDispatchRetryAfterClamped()
		}

		retryAfter = hint
	}

	retry.RetryAfterMS = int64(retryAfter / time.Millisecond)

	return retry
}

func (r *Runner) reissueFailedRequest(ctx context.Context, group alarmDispatchGroup, cause error) error {
	retry, terminal := prepareDispatchFailure(group.envelopes, cause)
	if len(terminal) == 0 {
		err := r.consumer.ReissueSendRequest(ctx, retry, group.request)
		if err == nil {
			return nil
		}

		if !errors.Is(err, sendfence.ErrGenerationExhausted) {
			return fmt.Errorf("persist reissued alarm request: %w", err)
		}
	}

	return r.quarantinePostSendingFailure(ctx, group.envelopes, fmt.Errorf("alarm request reissue budget exhausted: %w", cause))
}
