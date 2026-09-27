package dispatchrun

import (
	"errors"
	"fmt"
	"testing"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// dispatchoutbox.assignSendUnits가 한 send unit에 묶는 delivery 상한이다(maxDeliveriesPerSendUnit).
const alarmDispatchTestMaxDeliveriesPerSendUnit = 10

func alarmDispatchPersistedTextSendUnitEnvelopes(count int, build func() domain.AlarmQueueEnvelope) []domain.AlarmQueueEnvelope {
	envelopes := make([]domain.AlarmQueueEnvelope, 0, count)

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

// Text 경로는 send unit 전체를 한 메시지로 보내므로 저장된 send-unit client_request_id를 그대로 써야
// 재드레인 뒤에도 admission dedup이 같은 ID로 접힌다.
func TestAlarmDispatchTextPathSendsPersistedSendUnitClientRequestID(t *testing.T) {
	t.Parallel()

	for count := 1; count <= alarmDispatchTestMaxDeliveriesPerSendUnit; count++ {
		t.Run(fmt.Sprintf("envelopes=%d", count), func(t *testing.T) {
			t.Parallel()

			envelopes := alarmDispatchPersistedTextSendUnitEnvelopes(count, func() domain.AlarmQueueEnvelope {
				return alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
			})
			consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{envelopes}}
			sender := &alarmDispatchRunnerTestSender{}
			runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: alarmDispatchTestMaxDeliveriesPerSendUnit}

			processed, err := runner.runOnce(t.Context())

			require.NoError(t, err)
			assert.True(t, processed)
			require.Equal(t, []string{testRetryClientRequestID}, sender.clientRequestIDs,
				"text 경로는 send unit 크기와 무관하게 저장된 client_request_id를 보내야 한다")
			require.Len(t, consumer.markDispatched, count)
		})
	}
}

// 저장된 ID로 보낸 intrinsic text의 ambiguous 실패는 같은 ID로 재시도해야 한다(durable idempotency key).
func TestAlarmDispatchRunnerPersistedTextSendUnitRetriesAmbiguousFailure(t *testing.T) {
	t.Parallel()

	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection reset")}
	envelopes := alarmDispatchPersistedTextSendUnitEnvelopes(5, alarmDispatchRunnerIntrinsicTextEnvelope)
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{envelopes}}
	sender := &alarmDispatchRunnerTestSender{messageErr: transportErr}
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: alarmDispatchTestMaxDeliveriesPerSendUnit}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Equal(t, []string{testRetryClientRequestID}, sender.clientRequestIDs)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, len(envelopes))

	groups := groupAlarmDispatchEnvelopesForDelivery(consumer.scheduledSendingRetry)
	require.Len(t, groups, 1)

	retryID, err := alarmDispatchClientRequestID(groups[0])
	require.NoError(t, err)
	assert.Equal(t, testRetryClientRequestID, retryID)
}

// 재시도 허가(hasPersistedClientRequestID)는 Text 경로가 실제로 보낸 ID가 저장된 send-unit ID일 때만 참이어야 한다.
func TestHasPersistedClientRequestIDMatchesTextIDActuallySent(t *testing.T) {
	t.Parallel()

	for _, count := range []int{1, 4, 5, alarmDispatchTestMaxDeliveriesPerSendUnit} {
		envelopes := alarmDispatchPersistedTextSendUnitEnvelopes(count, func() domain.AlarmQueueEnvelope {
			return alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
		})
		group := alarmDispatchGroup{roomID: testAlarmRoomID, envelopes: envelopes}
		sentID, err := alarmDispatchClientRequestID(group)
		require.NoError(t, err)

		sentIsPersisted := sentID == envelopes[0].ClientRequestID

		assert.Equal(t, sentIsPersisted, hasPersistedClientRequestID(envelopes),
			"재시도 허가는 실제로 전송된 ClientRequestID가 persisted ID일 때만 참이어야 한다 (envelopes=%d)", count)
	}
}
