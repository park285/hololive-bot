package alarmdispatch

import (
	"errors"
	"testing"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

func TestAlarmDispatchRunnerMultiEnvelope503StillRetries(t *testing.T) {
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.DispatchOutboxID = 11

	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	second.DispatchOutboxID = 12

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{first, second}}}
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

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{first, second}}}
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
	assert.Equal(t, testRetryClientRequestID, groups[0].envelopes[0].ClientRequestID)
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

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{first, second}}}
	sender := &alarmDispatchRunnerTestSender{messageErr: transportErr}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 2)

	groups := groupAlarmDispatchEnvelopesForDelivery(consumer.scheduledSendingRetry)
	require.Len(t, groups, 1)
	assert.Equal(t, first.ClientRequestID, groups[0].envelopes[0].ClientRequestID)
}
