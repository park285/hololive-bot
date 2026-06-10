package workerapp

import (
	"context"
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type finalizeFailureCall struct {
	op        string
	envelopes []domain.AlarmQueueEnvelope
	claimKeys []string
}

type finalizeFailureRecordingConsumer struct {
	calls []finalizeFailureCall

	scheduleRetryErr        error
	scheduleSendingRetryErr error
	moveDLQErr              error
	releaseClaimErr         error
	requeueErr              error
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

func (c *finalizeFailureRecordingConsumer) ScheduleRetry(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.calls = append(c.calls, finalizeFailureCall{op: "ScheduleRetry", envelopes: envelopes})
	return c.scheduleRetryErr
}

func (c *finalizeFailureRecordingConsumer) MoveToDLQ(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.calls = append(c.calls, finalizeFailureCall{op: "MoveToDLQ", envelopes: envelopes})
	return c.moveDLQErr
}

func (c *finalizeFailureRecordingConsumer) Requeue(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.calls = append(c.calls, finalizeFailureCall{op: "Requeue", envelopes: envelopes})
	return c.requeueErr
}

type finalizeFailureSendingRetryConsumer struct {
	*finalizeFailureRecordingConsumer
}

func (c *finalizeFailureSendingRetryConsumer) ScheduleSendingRetry(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.calls = append(c.calls, finalizeFailureCall{op: "ScheduleSendingRetry", envelopes: envelopes})
	return c.scheduleSendingRetryErr
}

func finalizeFailureOps(calls []finalizeFailureCall) []string {
	ops := make([]string, 0, len(calls))
	for _, call := range calls {
		ops = append(ops, call.op)
	}
	return ops
}

func finalizeFailureRetryEnvelope(roomID string) domain.AlarmQueueEnvelope {
	env := alarmDispatchRunnerTestEnvelope(roomID, nil)
	env.ClaimKeys = []string{"claim:" + roomID}
	return env
}

func finalizeFailureDLQEnvelope(roomID string) domain.AlarmQueueEnvelope {
	env := alarmDispatchRunnerTestEnvelope(roomID, &domain.AlarmQueueRetryMetadata{Attempt: 2})
	env.ClaimKeys = []string{"claim:" + roomID}
	return env
}

func TestPersistPreSendFailureCallSequenceHappyPath(t *testing.T) {
	consumer := &finalizeFailureRecordingConsumer{}
	runner := alarmDispatchRunner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope("room-1"), finalizeFailureDLQEnvelope("room-2")}

	err := runner.persistPreSendFailure(t.Context(), envelopes, errors.New("render failed"))

	require.NoError(t, err)
	require.Equal(t, []string{"ScheduleRetry", "MoveToDLQ", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[0].envelopes, 1)
	assert.Equal(t, "room-1", consumer.calls[0].envelopes[0].Notification.RoomID)
	require.Len(t, consumer.calls[1].envelopes, 1)
	assert.Equal(t, "room-2", consumer.calls[1].envelopes[0].Notification.RoomID)
	assert.Equal(t, []string{"claim:room-2"}, consumer.calls[2].claimKeys)
}

func TestPersistPreSendFailureScheduleRetryFailureFallsBackToRequeue(t *testing.T) {
	scheduleErr := errors.New("schedule down")
	consumer := &finalizeFailureRecordingConsumer{scheduleRetryErr: scheduleErr}
	runner := alarmDispatchRunner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope("room-1"), finalizeFailureDLQEnvelope("room-2")}

	err := runner.persistPreSendFailure(t.Context(), envelopes, errors.New("render failed"))

	require.Error(t, err)
	assert.ErrorIs(t, err, scheduleErr)
	assert.Contains(t, err.Error(), "schedule alarm dispatch retry:")
	require.Equal(t, []string{"ScheduleRetry", "Requeue"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[1].envelopes, 1)
	assert.Equal(t, "room-1", consumer.calls[1].envelopes[0].Notification.RoomID)
}

func TestPersistPreSendFailureMoveDLQFailureFallsBackToRequeue(t *testing.T) {
	moveErr := errors.New("dlq down")
	consumer := &finalizeFailureRecordingConsumer{moveDLQErr: moveErr}
	runner := alarmDispatchRunner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope("room-1"), finalizeFailureDLQEnvelope("room-2")}

	err := runner.persistPreSendFailure(t.Context(), envelopes, errors.New("render failed"))

	require.Error(t, err)
	assert.ErrorIs(t, err, moveErr)
	assert.Contains(t, err.Error(), "move alarm dispatch dlq:")
	require.Equal(t, []string{"ScheduleRetry", "MoveToDLQ", "Requeue"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[2].envelopes, 1)
	assert.Equal(t, "room-2", consumer.calls[2].envelopes[0].Notification.RoomID)
}

func TestPersistPreSendFailureReleaseClaimKeysFailureWrapPinned(t *testing.T) {
	releaseErr := errors.New("release down")
	consumer := &finalizeFailureRecordingConsumer{releaseClaimErr: releaseErr}
	runner := alarmDispatchRunner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope("room-1"), finalizeFailureDLQEnvelope("room-2")}

	err := runner.persistPreSendFailure(t.Context(), envelopes, errors.New("render failed"))

	require.Error(t, err)
	assert.ErrorIs(t, err, releaseErr)
	assert.Equal(t, "release alarm dispatch dlq claim keys: release down", err.Error())
	require.Equal(t, []string{"ScheduleRetry", "MoveToDLQ", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
}

func TestPersistSendingRetryCallSequenceHappyPath(t *testing.T) {
	consumer := &finalizeFailureSendingRetryConsumer{finalizeFailureRecordingConsumer: &finalizeFailureRecordingConsumer{}}
	runner := alarmDispatchRunner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope("room-1"), finalizeFailureDLQEnvelope("room-2")}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.NoError(t, err)
	require.Equal(t, []string{"ScheduleSendingRetry", "MoveToDLQ", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[0].envelopes, 1)
	assert.Equal(t, "room-1", consumer.calls[0].envelopes[0].Notification.RoomID)
	require.Len(t, consumer.calls[1].envelopes, 1)
	assert.Equal(t, "room-2", consumer.calls[1].envelopes[0].Notification.RoomID)
	assert.Equal(t, []string{"claim:room-2"}, consumer.calls[2].claimKeys)
}

func TestPersistSendingRetryScheduleFailureFallsBackToRequeueWithPinnedWrap(t *testing.T) {
	scheduleErr := errors.New("sending retry down")
	consumer := &finalizeFailureSendingRetryConsumer{finalizeFailureRecordingConsumer: &finalizeFailureRecordingConsumer{scheduleSendingRetryErr: scheduleErr}}
	runner := alarmDispatchRunner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope("room-1"), finalizeFailureDLQEnvelope("room-2")}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.Error(t, err)
	assert.ErrorIs(t, err, scheduleErr)
	assert.Contains(t, err.Error(), "schedule alarm dispatch sending retry:")
	require.Equal(t, []string{"ScheduleSendingRetry", "Requeue"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[1].envelopes, 1)
	assert.Equal(t, "room-1", consumer.calls[1].envelopes[0].Notification.RoomID)
}

func TestPersistSendingRetryMoveDLQFailureFallsBackToRequeue(t *testing.T) {
	moveErr := errors.New("dlq down")
	consumer := &finalizeFailureSendingRetryConsumer{finalizeFailureRecordingConsumer: &finalizeFailureRecordingConsumer{moveDLQErr: moveErr}}
	runner := alarmDispatchRunner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope("room-1"), finalizeFailureDLQEnvelope("room-2")}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.Error(t, err)
	assert.ErrorIs(t, err, moveErr)
	assert.Contains(t, err.Error(), "move alarm dispatch dlq:")
	require.Equal(t, []string{"ScheduleSendingRetry", "MoveToDLQ", "Requeue"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[2].envelopes, 1)
	assert.Equal(t, "room-2", consumer.calls[2].envelopes[0].Notification.RoomID)
}

func TestPersistSendingRetryReleaseClaimKeysFailureWrapPinned(t *testing.T) {
	releaseErr := errors.New("release down")
	consumer := &finalizeFailureSendingRetryConsumer{finalizeFailureRecordingConsumer: &finalizeFailureRecordingConsumer{releaseClaimErr: releaseErr}}
	runner := alarmDispatchRunner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope("room-1"), finalizeFailureDLQEnvelope("room-2")}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.Error(t, err)
	assert.ErrorIs(t, err, releaseErr)
	assert.Equal(t, "release alarm dispatch dlq claim keys: release down", err.Error())
	require.Equal(t, []string{"ScheduleSendingRetry", "MoveToDLQ", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
}

func TestPersistSendingRetryCapabilityAbsentFallsBackToPreSendSequence(t *testing.T) {
	consumer := &finalizeFailureRecordingConsumer{}
	runner := alarmDispatchRunner{consumer: consumer}
	envelopes := []domain.AlarmQueueEnvelope{finalizeFailureRetryEnvelope("room-1"), finalizeFailureDLQEnvelope("room-2")}

	err := runner.persistSendingRetry(t.Context(), envelopes, errors.New("502"))

	require.NoError(t, err)
	require.Equal(t, []string{"ScheduleRetry", "MoveToDLQ", "ReleaseClaimKeys"}, finalizeFailureOps(consumer.calls))
	require.Len(t, consumer.calls[0].envelopes, 1)
	assert.Equal(t, "room-1", consumer.calls[0].envelopes[0].Notification.RoomID)
	assert.Equal(t, []string{"claim:room-2"}, consumer.calls[2].claimKeys)
}
