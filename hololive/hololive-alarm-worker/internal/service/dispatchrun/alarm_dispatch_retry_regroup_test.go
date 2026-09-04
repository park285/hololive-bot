package dispatchrun

import (
	"errors"
	"testing"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmDispatchRunnerMultiEnvelopeTransportFailureQuarantines(t *testing.T) {
	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection refused")}
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.DispatchOutboxID = 11

	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	second.DispatchOutboxID = 12

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{first, second}}}
	sender := &alarmDispatchRunnerTestSender{karingErr: transportErr}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.quarantined, 2, "multi-envelope ambiguous failure must not re-enter the regrouping drain")
	assert.Empty(t, consumer.scheduledSendingRetry)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerMultiEnvelope503StillRetries(t *testing.T) {
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.DispatchOutboxID = 11

	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	second.DispatchOutboxID = 12

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{first, second}}}
	sender := &alarmDispatchRunnerTestSender{karingErr: &iris.HTTPError{StatusCode: 503}}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 2, "definitive not-admitted status keeps group-size-independent retry")
}

func TestAlarmDispatchRunnerPersistedSendUnitQuarantinesAmbiguousRoomScopedFailure(t *testing.T) {
	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection reset")}
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.DispatchOutboxID = 11
	first.SendUnitID = 7
	first.ClientRequestID = testRetryClientRequestID

	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	second.DispatchOutboxID = 12
	second.SendUnitID = first.SendUnitID
	second.ClientRequestID = first.ClientRequestID

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{first, second}}}
	sender := &alarmDispatchRunnerTestSender{karingErr: transportErr}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.quarantined, 2)
	assert.Empty(t, consumer.scheduledSendingRetry)
}

func TestAlarmDispatchRunnerPersistedSendUnitRetriesAmbiguousIntrinsicTextFailure(t *testing.T) {
	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection reset")}
	first := alarmDispatchRunnerIntrinsicTextEnvelope(testAlarmRoomID)

	first.DispatchOutboxID = 11
	first.SendUnitID = 7
	first.ClientRequestID = testRetryClientRequestID

	second := alarmDispatchRunnerIntrinsicTextEnvelope(testAlarmRoomID)

	second.DispatchOutboxID = 12
	second.SendUnitID = first.SendUnitID
	second.ClientRequestID = first.ClientRequestID

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{first, second}}}
	sender := &alarmDispatchRunnerTestSender{messageErr: transportErr}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 2)

	groups := groupAlarmDispatchEnvelopesForDelivery(t.Context(), sender, consumer.scheduledSendingRetry)
	require.Len(t, groups, 1)
	assert.Equal(t, first.ClientRequestID, alarmDispatchClientRequestID(groups[0], 0, len(groups[0].envelopes)))
}

func TestAlarmDispatchPostSendFailureIsRetryable(t *testing.T) {
	t.Parallel()

	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection reset by peer")}
	assert.True(t, alarmDispatchPostSendFailureIsRetryable(transportErr, 1))
	assert.False(t, alarmDispatchPostSendFailureIsRetryable(transportErr, 2))
	assert.True(t, alarmDispatchPostSendFailureIsRetryable(&iris.HTTPError{StatusCode: 503}, 2))
	assert.False(t, alarmDispatchPostSendFailureIsRetryable(&iris.HTTPError{StatusCode: 500}, 1))
	assert.False(t, alarmDispatchPostSendFailureIsRetryable(nil, 1))
}

func TestGroupAlarmDispatchEnvelopesKeepsRetriedEnvelopeSolo(t *testing.T) {
	t.Parallel()

	retried := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &domain.AlarmQueueRetryMetadata{Attempt: 1})

	retried.DispatchOutboxID = 21

	freshA := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	freshA.DispatchOutboxID = 22

	freshB := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	freshB.DispatchOutboxID = 23

	groups := groupAlarmDispatchEnvelopesForDelivery(t.Context(), &alarmDispatchRunnerTestSender{}, []domain.AlarmQueueEnvelope{retried, freshA, freshB})

	require.Len(t, groups, 2)
	require.Len(t, groups[0].envelopes, 1)
	assert.Equal(t, int64(21), groups[0].envelopes[0].DispatchOutboxID)
	require.Len(t, groups[1].envelopes, 2)
}

func TestAlarmDispatchClientRequestIDStableAcrossSoloRedrain(t *testing.T) {
	t.Parallel()

	original := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	original.DispatchOutboxID = 31

	firstGroups := groupAlarmDispatchEnvelopesForDelivery(t.Context(), &alarmDispatchRunnerTestSender{}, []domain.AlarmQueueEnvelope{original})
	require.Len(t, firstGroups, 1)

	firstID := alarmDispatchClientRequestID(firstGroups[0], 0, len(firstGroups[0].envelopes))

	redrained := original

	redrained.Retry = &domain.AlarmQueueRetryMetadata{Attempt: 1, LastError: "transport"}

	fresh := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	fresh.DispatchOutboxID = 32

	retryGroups := groupAlarmDispatchEnvelopesForDelivery(t.Context(), &alarmDispatchRunnerTestSender{}, []domain.AlarmQueueEnvelope{fresh, redrained})

	require.Len(t, retryGroups, 2)

	var soloGroup *alarmDispatchGroup

	for i := range retryGroups {
		if len(retryGroups[i].envelopes) == 1 && retryGroups[i].envelopes[0].DispatchOutboxID == 31 {
			soloGroup = &retryGroups[i]
		}
	}

	require.NotNil(t, soloGroup, "retried envelope must re-drain as a solo group")

	retryID := alarmDispatchClientRequestID(*soloGroup, 0, len(soloGroup.envelopes))
	assert.Equal(t, firstID, retryID, "solo redrain must reproduce the first-send ClientRequestID so Iris dedup folds an admitted resend")
}
