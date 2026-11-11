package alarmdispatch

import (
	"errors"
	"fmt"
	"testing"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

// dispatchoutbox.assignSendUnits가 한 send unit에 묶는 delivery 상한이다(maxDeliveriesPerSendUnit).
const alarmDispatchTestMaxDeliveriesPerSendUnit = 10

func alarmDispatchPersistedTextSendUnitEnvelopes(count int, build func() dispatchoutbox.AlarmQueueEnvelope) []dispatchoutbox.AlarmQueueEnvelope {
	envelopes := make([]dispatchoutbox.AlarmQueueEnvelope, 0, count)

	for i := range count {
		envelope := build()

		envelope.DispatchOutboxID = int64(11 + i)
		envelope.SendUnitID = 7
		envelope.ClientRequestID = testRetryClientRequestID

		if envelope.Notification.Stream != nil {
			stream := *envelope.Notification.Stream

			stream.ID = fmt.Sprintf("stream-%d", 11+i)
			envelope.Notification.Stream = &stream
		}

		envelopes = append(envelopes, envelope)
	}

	return envelopes
}

func TestAlarmDispatchTextPathSendsPersistedSendUnitClientRequestID(t *testing.T) {
	t.Parallel()

	for count := 1; count <= alarmDispatchTestMaxDeliveriesPerSendUnit; count++ {
		t.Run(fmt.Sprintf("envelopes=%d", count), func(t *testing.T) {
			t.Parallel()

			envelopes := alarmDispatchPersistedTextSendUnitEnvelopes(count, func() dispatchoutbox.AlarmQueueEnvelope {
				return alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
			})
			consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{envelopes}}
			sender := &alarmDispatchRunnerTestSender{}
			runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: alarmDispatchTestMaxDeliveriesPerSendUnit}

			processed, err := runner.runOnce(t.Context())

			require.NoError(t, err)
			assert.True(t, processed)
			require.Equal(t, []string{testRetryClientRequestID}, sender.clientRequestIDs,
				"text 경로는 send unit 크기와 무관하게 저장된 client_request_id를 보내야 한다")
			require.Len(t, consumer.markDispatched, count)
			require.Equal(t, []string{"iris:" + testRetryClientRequestID}, consumer.markDispatchedRequestIDs,
				"sent 기록은 202가 돌려준 Iris requestId를 handoff 확인 대상으로 넘겨야 한다")
		})
	}
}

func TestAlarmDispatchRunnerPersistedTextSendUnitRetriesAmbiguousFailure(t *testing.T) {
	t.Parallel()

	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection reset")}
	envelopes := alarmDispatchPersistedTextSendUnitEnvelopes(5, alarmDispatchRunnerIntrinsicTextEnvelope)
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{envelopes}}
	sender := &alarmDispatchRunnerTestSender{messageErr: transportErr}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: alarmDispatchTestMaxDeliveriesPerSendUnit}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Equal(t, []string{testRetryClientRequestID}, sender.clientRequestIDs)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, len(envelopes))

	groups := groupAlarmDispatchEnvelopesForDelivery(consumer.scheduledSendingRetry)
	require.Len(t, groups, 1)
	assert.Equal(t, testRetryClientRequestID, groups[0].envelopes[0].ClientRequestID)
}
