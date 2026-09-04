package dispatchrun

import (
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmDispatchRunnerRetryable502AfterMarkSendingUsesRouteSendingFailures(t *testing.T) {
	karingErr := &iris.HTTPError{StatusCode: 502, URL: testKaringContentListPath}

	consumer := &alarmDispatchRunnerTestConsumer{
		batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}},
	}
	sender := &alarmDispatchRunnerTestSender{karingErr: karingErr}
	runner := Runner{
		consumer: consumer,
		sender:   sender,
		maxBatch: 10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.scheduledSendingRetry, 1, "502 post-send failure must route through RouteSendingFailures, not RouteFailures")
	require.NotNil(t, consumer.scheduledSendingRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledSendingRetry[0].Retry.Attempt)
	assert.Empty(t, consumer.scheduledRetry, "RouteFailures must not be called for post-send failure when row is already 'sending'")
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerRetryable503AfterMarkSendingUsesRouteSendingFailures(t *testing.T) {
	karingErr := &iris.HTTPError{StatusCode: 503}

	consumer := &alarmDispatchRunnerTestConsumer{
		batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}},
	}
	sender := &alarmDispatchRunnerTestSender{karingErr: karingErr}
	runner := Runner{
		consumer: consumer,
		sender:   sender,
		maxBatch: 10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledSendingRetry, 1, "503 post-send failure must route through RouteSendingFailures")
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.quarantined)
}

func TestPrepareDispatchFailureUsesHTTPRetryAfterHintWhenLongerThanAttemptDelay(t *testing.T) {
	for _, statusCode := range []int{503, 429} {
		t.Run((&iris.HTTPError{StatusCode: statusCode}).Error(), func(t *testing.T) {
			envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
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
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &domain.AlarmQueueRetryMetadata{Attempt: 1})
	cause := &iris.HTTPError{StatusCode: 503, RetryAfter: time.Second}
	startedAt := time.Now().UTC()

	retry := nextAlarmDispatchRetry(&envelope, cause)

	require.NotNil(t, retry)
	assert.Equal(t, 2, retry.Attempt)
	assert.Equal(t, int64(10000), retry.RetryAfterMS)
	assertRetryNextVisibleDelay(t, retry, startedAt, 10*time.Second)
}

func TestNextAlarmDispatchRetryClampsExcessiveHTTPRetryAfter(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
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
