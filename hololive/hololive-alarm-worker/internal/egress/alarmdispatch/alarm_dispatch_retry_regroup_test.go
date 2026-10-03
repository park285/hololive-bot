package alarmdispatch

import (
	"errors"
	"testing"

	"github.com/park285/iris-client-go/v3/iris"
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
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 2, "definitive not-admitted status keeps group-size-independent retry")
}

// 방송 알림 그룹도 저장된 send-unit ID로 보내므로 ambiguous 실패를 같은 ID로 재시도한다. Karing과 Text 사이의 경로 전환이
// 없어져(DEC-20260926-hololive-karing-egress-disposition) 방 기준 그룹을 따로 quarantine할 이유가 없다.
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
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

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
	runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

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

// migration 141 이전 delivery를 claim하던 legacy_head와 그룹 구성에서 ID를 파생하던 폴백을 지웠다. 저장된
// send-unit client_request_id가 없는 그룹은 파생 ID로 보내지 않고 발송 전 실패로 드러낸다.
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
			runner := Runner{consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

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
