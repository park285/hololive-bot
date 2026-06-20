package workerapp

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlarmDispatchRunnerRetryable502AfterMarkSendingUsesScheduleSendingRetry(t *testing.T) {
	karingErr := &iris.HTTPError{StatusCode: 502, URL: "/karing/content-list"}

	consumer := &alarmDispatchRunnerSendingRetryTestConsumer{
		batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}},
	}
	sender := &alarmDispatchRunnerTestSender{karingErr: karingErr}
	runner := alarmDispatchRunner{
		consumer:           consumer,
		sender:             sender,
		karingEnabled:      true,
		postSendQuarantine: true,
		maxBatch:           10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.scheduledSendingRetry, 1, "502 post-send failure must route through ScheduleSendingRetry, not ScheduleRetry")
	require.NotNil(t, consumer.scheduledSendingRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledSendingRetry[0].Retry.Attempt)
	assert.Empty(t, consumer.scheduledRetry, "ScheduleRetry must not be called for post-send failure when row is already 'sending'")
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerRetryable503AfterMarkSendingUsesScheduleSendingRetry(t *testing.T) {
	karingErr := &iris.HTTPError{StatusCode: 503}

	consumer := &alarmDispatchRunnerSendingRetryTestConsumer{
		batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}},
	}
	sender := &alarmDispatchRunnerTestSender{karingErr: karingErr}
	runner := alarmDispatchRunner{
		consumer:           consumer,
		sender:             sender,
		karingEnabled:      true,
		postSendQuarantine: true,
		maxBatch:           10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledSendingRetry, 1, "503 post-send failure must route through ScheduleSendingRetry")
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.quarantined)
}

func TestPrepareDispatchFailureUsesHTTPRetryAfterHintWhenLongerThanAttemptDelay(t *testing.T) {
	for _, statusCode := range []int{503, 429} {
		t.Run((&iris.HTTPError{StatusCode: statusCode}).Error(), func(t *testing.T) {
			envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
			cause := &iris.HTTPError{StatusCode: statusCode, RetryAfter: 12 * time.Second}
			startedAt := time.Now().UTC()

			retryEnvelopes, dlqEnvelopes := prepareDispatchFailure([]domain.AlarmQueueEnvelope{envelope}, cause)

			require.Empty(t, dlqEnvelopes)
			require.Len(t, retryEnvelopes, 1)
			require.NotNil(t, retryEnvelopes[0].Retry)
			assert.Equal(t, 1, retryEnvelopes[0].Retry.Attempt)
			assert.Equal(t, int64(12000), retryEnvelopes[0].Retry.RetryAfterMS)
			assertRetryNextVisibleDelay(t, retryEnvelopes[0].Retry, startedAt, 12*time.Second)
		})
	}
}

func TestNextAlarmDispatchRetryKeepsAttemptDelayWhenHTTPRetryAfterHintIsShorter(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", &domain.AlarmQueueRetryMetadata{Attempt: 1})
	cause := &iris.HTTPError{StatusCode: 503, RetryAfter: time.Second}
	startedAt := time.Now().UTC()

	retry := nextAlarmDispatchRetry(&envelope, cause)

	require.NotNil(t, retry)
	assert.Equal(t, 2, retry.Attempt)
	assert.Equal(t, int64(10000), retry.RetryAfterMS)
	assertRetryNextVisibleDelay(t, retry, startedAt, 10*time.Second)
}

func TestNextAlarmDispatchRetryClampsExcessiveHTTPRetryAfter(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	cause := &iris.HTTPError{StatusCode: 503, RetryAfter: 24 * time.Hour}
	startedAt := time.Now().UTC()

	retry := nextAlarmDispatchRetry(&envelope, cause)

	require.NotNil(t, retry)
	assert.Equal(t, 1, retry.Attempt)
	assert.Equal(t, int64(maxHTTPRetryAfter/time.Millisecond), retry.RetryAfterMS,
		"excessive Retry-After must clamp to maxHTTPRetryAfter")
	assertRetryNextVisibleDelay(t, retry, startedAt, maxHTTPRetryAfter)
}

func assertRetryNextVisibleDelay(t *testing.T, retry *domain.AlarmQueueRetryMetadata, startedAt time.Time, delay time.Duration) {
	t.Helper()
	nextVisibleAt, err := time.Parse(time.RFC3339Nano, retry.NextVisibleAt)
	require.NoError(t, err)
	assert.False(t, nextVisibleAt.Before(startedAt.Add(delay)), "NextVisibleAt %s should be at least %s after start", nextVisibleAt, delay)
	assert.False(t, nextVisibleAt.After(time.Now().UTC().Add(delay+200*time.Millisecond)), "NextVisibleAt %s should stay near RetryAfterMS delay %s", nextVisibleAt, delay)
}

type alarmDispatchRunnerSendingRetryTestConsumer struct {
	batches               [][]domain.AlarmQueueEnvelope
	markSending           []domain.AlarmQueueEnvelope
	markDispatched        []domain.AlarmQueueEnvelope
	quarantined           []domain.AlarmQueueEnvelope
	quarantineReason      string
	scheduledRetry        []domain.AlarmQueueEnvelope
	scheduledSendingRetry []domain.AlarmQueueEnvelope
	movedDLQ              []domain.AlarmQueueEnvelope
	requeued              []domain.AlarmQueueEnvelope
	releasedClaims        []string
}

func (c *alarmDispatchRunnerSendingRetryTestConsumer) DrainBatch(_ context.Context, _ int) ([]domain.AlarmQueueEnvelope, error) {
	if len(c.batches) == 0 {
		return nil, nil
	}
	batch := c.batches[0]
	c.batches = c.batches[1:]
	return batch, nil
}

func (c *alarmDispatchRunnerSendingRetryTestConsumer) MarkSending(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markSending = append(c.markSending, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerSendingRetryTestConsumer) MarkDispatched(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markDispatched = append(c.markDispatched, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerSendingRetryTestConsumer) ReleaseClaimKeys(_ context.Context, claimKeys []string) error {
	c.releasedClaims = append(c.releasedClaims, claimKeys...)
	return nil
}

func (c *alarmDispatchRunnerSendingRetryTestConsumer) ScheduleRetry(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.scheduledRetry = append(c.scheduledRetry, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerSendingRetryTestConsumer) ScheduleSendingRetry(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.scheduledSendingRetry = append(c.scheduledSendingRetry, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerSendingRetryTestConsumer) MoveToDLQ(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.movedDLQ = append(c.movedDLQ, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerSendingRetryTestConsumer) Requeue(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.requeued = append(c.requeued, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerSendingRetryTestConsumer) Quarantine(_ context.Context, envelopes []domain.AlarmQueueEnvelope, reason string) error {
	c.quarantined = append(c.quarantined, envelopes...)
	c.quarantineReason = reason
	return nil
}
