package dispatchrun

import (
	"context"
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type finalizeFailureCall struct {
	op        string
	retry     []domain.AlarmQueueEnvelope
	dlq       []domain.AlarmQueueEnvelope
	envelopes []domain.AlarmQueueEnvelope
	claimKeys []string
}

type finalizeFailureRecordingConsumer struct {
	calls []finalizeFailureCall

	routeFailuresErr         error
	routeSendingFailuresErrs []error
	releaseClaimErr          error
	requeueErr               error
}

func (c *finalizeFailureRecordingConsumer) DrainBatch(context.Context, int) ([]domain.AlarmQueueEnvelope, error) {
	return nil, nil
}

func (c *finalizeFailureRecordingConsumer) MarkSending(context.Context, []domain.AlarmQueueEnvelope) error {
	return nil
}

func (c *finalizeFailureRecordingConsumer) MarkDispatched(context.Context, []domain.AlarmQueueEnvelope) error {
	return nil
}

func (c *finalizeFailureRecordingConsumer) ReleaseClaimKeys(_ context.Context, claimKeys []string) error {
	c.calls = append(c.calls, finalizeFailureCall{op: "ReleaseClaimKeys", claimKeys: claimKeys})
	return c.releaseClaimErr
}

func (c *finalizeFailureRecordingConsumer) RouteFailures(_ context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	c.calls = append(c.calls, finalizeFailureCall{op: "RouteFailures", retry: retryEnvelopes, dlq: dlqEnvelopes})
	return c.routeFailuresErr
}

func (c *finalizeFailureRecordingConsumer) Requeue(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.calls = append(c.calls, finalizeFailureCall{op: "Requeue", envelopes: envelopes})
	return c.requeueErr
}

type finalizeFailureSendingRetryConsumer struct {
	*finalizeFailureRecordingConsumer
}

func (c *finalizeFailureSendingRetryConsumer) RouteSendingFailures(_ context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	c.calls = append(c.calls, finalizeFailureCall{op: "RouteSendingFailures", retry: retryEnvelopes, dlq: dlqEnvelopes})
	if len(c.routeSendingFailuresErrs) == 0 {
		return nil
	}
	err := c.routeSendingFailuresErrs[0]
	c.routeSendingFailuresErrs = c.routeSendingFailuresErrs[1:]
	return err
}

func finalizeFailureOps(calls []finalizeFailureCall) []string {
	ops := make([]string, 0, len(calls))
	for _, call := range calls {
		ops = append(ops, call.op)
	}
	return ops
}

const (
	finalizeFailureRetryDeliveryID = int64(11)
	finalizeFailureDLQDeliveryID   = int64(22)
)

func finalizeFailureRetryEnvelope() domain.AlarmQueueEnvelope {
	roomID := "room-1"
	env := alarmDispatchRunnerTestEnvelope(roomID, nil)
	env.DispatchOutboxID = finalizeFailureRetryDeliveryID
	env.ClaimKeys = []string{"claim:" + roomID}
	return env
}

func finalizeFailureDLQEnvelope() domain.AlarmQueueEnvelope {
	roomID := "room-2"
	env := alarmDispatchRunnerTestEnvelope(roomID, &domain.AlarmQueueRetryMetadata{Attempt: 2})
	env.DispatchOutboxID = finalizeFailureDLQDeliveryID
	env.ClaimKeys = []string{"claim:" + roomID}
	return env
}

func TestPersistPreSendFailureCallSequenceHappyPath(t *testing.T) {
	consumer := &finalizeFailureRecordingConsumer{}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistPreSendFailure(t.Context(), envelopes, errors.New("render failed"))

	require.NoError(t, err)
	require.Equal(t, []string{"RouteFailures", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[0].retry, 1)
	assert.Equal(t, "room-1", consumer.calls[0].retry[0].Notification.RoomID)
	require.Len(t, consumer.calls[0].dlq, 1)
	assert.Equal(t, "room-2", consumer.calls[0].dlq[0].Notification.RoomID)
	assert.Equal(t, []string{"claim:room-2"}, consumer.calls[1].claimKeys)
}

func TestPersistPreSendFailureRouteFailureFallsBackToRequeueWithAllEnvelopes(t *testing.T) {
	routeErr := errors.New("route down")
	consumer := &finalizeFailureRecordingConsumer{routeFailuresErr: routeErr}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistPreSendFailure(t.Context(), envelopes, errors.New("render failed"))

	require.Error(t, err)
	assert.ErrorIs(t, err, routeErr)
	assert.Contains(t, err.Error(), "route alarm dispatch failure:")
	require.Equal(t, []string{"RouteFailures", "Requeue"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[1].envelopes, 2)
	assert.Equal(t, "room-1", consumer.calls[1].envelopes[0].Notification.RoomID)
	assert.Equal(t, "room-2", consumer.calls[1].envelopes[1].Notification.RoomID)
}

func TestPersistPreSendFailureReleaseClaimKeysFailureWrapPinned(t *testing.T) {
	releaseErr := errors.New("release down")
	consumer := &finalizeFailureRecordingConsumer{releaseClaimErr: releaseErr}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistPreSendFailure(t.Context(), envelopes, errors.New("render failed"))

	require.Error(t, err)
	assert.ErrorIs(t, err, releaseErr)
	assert.Equal(t, "release alarm dispatch dlq claim keys: release down", err.Error())
	require.Equal(t, []string{"RouteFailures", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
}

func TestPersistPreSendFailurePartialRoutingSkipsRequeueAndReleasesAppliedDLQ(t *testing.T) {
	partialErr := &dispatchoutbox.PartialTransitionError{
		Action:       "route dispatch delivery failures",
		Updated:      1,
		Expected:     2,
		UnappliedIDs: []int64{finalizeFailureRetryDeliveryID},
	}
	consumer := &finalizeFailureRecordingConsumer{routeFailuresErr: partialErr}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistPreSendFailure(t.Context(), envelopes, errors.New("render failed"))

	require.Error(t, err)
	assert.ErrorIs(t, err, partialErr)
	require.Equal(t, []string{"RouteFailures", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls),
		"partial routing must not requeue: unapplied rows are owned by recovery/quarantine")
	assert.Equal(t, []string{"claim:room-2"}, consumer.calls[1].claimKeys,
		"applied dlq row's claim keys must still be released")
}

func TestPersistPreSendFailurePartialRoutingKeepsUnappliedDLQClaims(t *testing.T) {
	partialErr := &dispatchoutbox.PartialTransitionError{
		Action:       "route dispatch delivery failures",
		Updated:      1,
		Expected:     2,
		UnappliedIDs: []int64{finalizeFailureDLQDeliveryID},
	}
	consumer := &finalizeFailureRecordingConsumer{routeFailuresErr: partialErr}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistPreSendFailure(t.Context(), envelopes, errors.New("render failed"))

	require.Error(t, err)
	assert.ErrorIs(t, err, partialErr)
	require.Equal(t, []string{"RouteFailures", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
	assert.Empty(t, consumer.calls[1].claimKeys,
		"unapplied dlq row's claim keys must not be released")
}

func TestPersistSendingRetryCallSequenceHappyPath(t *testing.T) {
	consumer := &finalizeFailureSendingRetryConsumer{finalizeFailureRecordingConsumer: &finalizeFailureRecordingConsumer{}}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.NoError(t, err)
	require.Equal(t, []string{"RouteSendingFailures", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[0].retry, 1)
	assert.Equal(t, "room-1", consumer.calls[0].retry[0].Notification.RoomID)
	require.Len(t, consumer.calls[0].dlq, 1)
	assert.Equal(t, "room-2", consumer.calls[0].dlq[0].Notification.RoomID)
	assert.Equal(t, []string{"claim:room-2"}, consumer.calls[1].claimKeys)
}

func TestPersistSendingRetryInfraFailureFallsBackToSendingFenceRequeue(t *testing.T) {
	routeErr := errors.New("sending route down")
	consumer := &finalizeFailureSendingRetryConsumer{finalizeFailureRecordingConsumer: &finalizeFailureRecordingConsumer{routeSendingFailuresErrs: []error{routeErr}}}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.Error(t, err)
	assert.ErrorIs(t, err, routeErr)
	assert.Contains(t, err.Error(), "route alarm dispatch sending failure:")
	require.Equal(t, []string{"RouteSendingFailures", "RouteSendingFailures"}, finalizeFailureOps(consumer.calls),
		"sending 경로 fallback은 leased 전용 Requeue가 아니라 sending fence 전량-retry로 복원해야 한다")
	require.Len(t, consumer.calls[1].retry, 2)
	assert.Equal(t, "room-1", consumer.calls[1].retry[0].Notification.RoomID)
	assert.Equal(t, "room-2", consumer.calls[1].retry[1].Notification.RoomID)
	assert.Empty(t, consumer.calls[1].dlq, "fallback requeue는 전량 retry로 복원한다")
}

func TestPersistSendingRetryFallbackRequeueFailureWrapPinned(t *testing.T) {
	routeErr := errors.New("sending route down")
	requeueErr := errors.New("sending requeue down")
	consumer := &finalizeFailureSendingRetryConsumer{finalizeFailureRecordingConsumer: &finalizeFailureRecordingConsumer{routeSendingFailuresErrs: []error{routeErr, requeueErr}}}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.Error(t, err)
	assert.ErrorIs(t, err, routeErr)
	assert.ErrorIs(t, err, requeueErr)
	assert.Contains(t, err.Error(), "fallback requeue:")
	require.Equal(t, []string{"RouteSendingFailures", "RouteSendingFailures"}, finalizeFailureOps(consumer.calls))
}

func TestPersistSendingRetryPartialRoutingSkipsRequeueAndReleasesAppliedDLQ(t *testing.T) {
	partialErr := &dispatchoutbox.PartialTransitionError{
		Action:       "route dispatch delivery sending failures",
		Updated:      1,
		Expected:     2,
		UnappliedIDs: []int64{finalizeFailureRetryDeliveryID},
	}
	consumer := &finalizeFailureSendingRetryConsumer{finalizeFailureRecordingConsumer: &finalizeFailureRecordingConsumer{routeSendingFailuresErrs: []error{partialErr}}}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.Error(t, err)
	assert.ErrorIs(t, err, partialErr)
	require.Equal(t, []string{"RouteSendingFailures", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
	assert.Equal(t, []string{"claim:room-2"}, consumer.calls[1].claimKeys)
}

func TestPersistSendingRetryReleaseClaimKeysFailureWrapPinned(t *testing.T) {
	releaseErr := errors.New("release down")
	consumer := &finalizeFailureSendingRetryConsumer{finalizeFailureRecordingConsumer: &finalizeFailureRecordingConsumer{releaseClaimErr: releaseErr}}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.Error(t, err)
	assert.ErrorIs(t, err, releaseErr)
	assert.Equal(t, "release alarm dispatch dlq claim keys: release down", err.Error())
	require.Equal(t, []string{"RouteSendingFailures", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
}

func TestPersistSendingRetryCapabilityAbsentFallsBackToPreSendSequence(t *testing.T) {
	consumer := &finalizeFailureRecordingConsumer{}
	runner := Runner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope(), finalizeFailureDLQEnvelope()}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.NoError(t, err)
	require.Equal(t, []string{"RouteFailures", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[0].retry, 1)
	assert.Equal(t, "room-1", consumer.calls[0].retry[0].Notification.RoomID)
	assert.Equal(t, []string{"claim:room-2"}, consumer.calls[1].claimKeys)
}
