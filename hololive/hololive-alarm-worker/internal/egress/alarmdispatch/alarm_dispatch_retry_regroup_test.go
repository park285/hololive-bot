package alarmdispatch

import (
	"errors"
	"testing"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmDispatchRunnerMultiEnvelope503StillRetries(t *testing.T) {
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.DispatchOutboxID = 11

	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	second.DispatchOutboxID = 12

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{first, second}}}
	sender := &alarmDispatchRunnerTestSender{messageErr: &iris.HTTPError{StatusCode: 503}}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 2, "definitive not-admitted status keeps group-size-independent retry")
}

func TestAlarmDispatchRunnerPersistedSendUnitRetriesAmbiguousStreamFailure(t *testing.T) {
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
	sender := &alarmDispatchRunnerTestSender{messageErr: transportErr}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Equal(t, []string{testRetryClientRequestID}, sender.clientRequestIDs)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 2)

	groups := groupAlarmDispatchEnvelopesForDelivery(consumer.scheduledSendingRetry)
	require.Len(t, groups, 1)

	retryID, err := alarmDispatchClientRequestID(groups[0])
	require.NoError(t, err)
	assert.Equal(t, testRetryClientRequestID, retryID)
}

func TestAlarmDispatchRunnerPersistedSendUnitRetriesAmbiguousIntrinsicTextFailure(t *testing.T) {
	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection reset")}
	first := alarmDispatchRunnerIntrinsicTextEnvelope()

	first.DispatchOutboxID = 11
	first.SendUnitID = 7
	first.ClientRequestID = testRetryClientRequestID

	second := alarmDispatchRunnerIntrinsicTextEnvelope()

	second.DispatchOutboxID = 12
	second.SendUnitID = first.SendUnitID
	second.ClientRequestID = first.ClientRequestID

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{first, second}}}
	sender := &alarmDispatchRunnerTestSender{messageErr: transportErr}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 2)

	groups := groupAlarmDispatchEnvelopesForDelivery(consumer.scheduledSendingRetry)
	require.Len(t, groups, 1)

	retryID, err := alarmDispatchClientRequestID(groups[0])
	require.NoError(t, err)
	assert.Equal(t, first.ClientRequestID, retryID)
}

func TestAlarmDispatchRunnerRefusesGroupWithoutPersistedSendUnitIdentity(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		configure func(first, second *domain.AlarmQueueEnvelope)
	}{
		{
			name:      "missing",
			configure: func(_, _ *domain.AlarmQueueEnvelope) {},
		},
		{
			name: "mismatched",
			configure: func(first, second *domain.AlarmQueueEnvelope) {
				first.ClientRequestID = testRetryClientRequestID
				second.ClientRequestID = testRetryClientRequestID + "-other"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &domain.AlarmQueueRetryMetadata{Attempt: alarmDispatchMaxAttempts})
			second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &domain.AlarmQueueRetryMetadata{Attempt: alarmDispatchMaxAttempts})

			first.DispatchOutboxID = 41
			first.SendUnitID = 9
			second.DispatchOutboxID = 42
			second.SendUnitID = 9
			tc.configure(&first, &second)

			consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{first, second}}}
			sender := &alarmDispatchRunnerTestSender{}
			runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

			processed, err := runner.runOnce(t.Context())

			require.NoError(t, err)
			assert.True(t, processed)
			assert.Empty(t, sender.messages)
			assert.Empty(t, sender.clientRequestIDs)
			assert.Empty(t, consumer.markSending)
			require.Len(t, consumer.movedDLQ, 2)
		})
	}
}

func TestAlarmDispatchClientRequestIDRequiresPersistedSendUnitIdentity(t *testing.T) {
	t.Parallel()

	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	_, err := alarmDispatchClientRequestID(alarmDispatchGroup{roomID: testAlarmRoomID, envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.ErrorIs(t, err, errAlarmDispatchSendUnitIdentityMissing)

	envelope.SendUnitID = 5
	envelope.ClientRequestID = testRetryClientRequestID

	got, err := alarmDispatchClientRequestID(alarmDispatchGroup{roomID: testAlarmRoomID, envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)
	assert.Equal(t, testRetryClientRequestID, got)
}
