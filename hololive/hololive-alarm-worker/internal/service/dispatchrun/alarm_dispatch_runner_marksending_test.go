package dispatchrun

import (
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errAlarmDispatchRunnerTestMarkSending = errors.New("mark sending partial update")

func TestAlarmDispatchRunnerCompensatesMarkSendingFailureWithoutConsumingAttempt(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:        [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}},
		markSendingErr: errAlarmDispatchRunnerTestMarkSending,
	}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{
		consumer: consumer,
		sender:   sender,
		renderer: newAlarmDispatchTestRenderer(t),
		maxBatch: 10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, sender.messages, "메시지는 발송되면 안 된다")
	require.Len(t, consumer.preSendRequeued, 1)
	require.NotNil(t, consumer.preSendRequeued[0].Retry)
	assert.Equal(t, 0, consumer.preSendRequeued[0].Retry.Attempt)
	assert.Contains(t, consumer.preSendRequeued[0].Retry.LastError, errAlarmDispatchRunnerTestMarkSending.Error())
	assert.Empty(t, consumer.scheduledSendingRetry, "외부 발송 후 실패 경로를 사용하면 attempt를 소비한다")
	assert.Empty(t, consumer.scheduledRetry, "leased 전용 RouteFailures로 보상하면 sending 행이 잔류한다")
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerCompensatesKaringMarkSendingFailureWithSendingRetry(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:        [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}},
		markSendingErr: errAlarmDispatchRunnerTestMarkSending,
	}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{
		consumer:      consumer,
		sender:        sender,
		karingEnabled: true,
		maxBatch:      10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, sender.karingRequests, "karing 요청은 발송되면 안 된다")
	require.Len(t, consumer.preSendRequeued, 1)
	assert.Equal(t, 0, consumer.preSendRequeued[0].Retry.Attempt)
	assert.Empty(t, consumer.scheduledSendingRetry)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerMarkSendingFailureDoesNotExhaustExistingAttempt(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", &domain.AlarmQueueRetryMetadata{Attempt: 2})
	envelope.ClaimKeys = []string{"alarm:dispatch:claim:room-1:stream-1"}
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:        [][]domain.AlarmQueueEnvelope{{envelope}},
		markSendingErr: errAlarmDispatchRunnerTestMarkSending,
	}
	runner := Runner{
		consumer: consumer,
		sender:   &alarmDispatchRunnerTestSender{},
		renderer: newAlarmDispatchTestRenderer(t),
		maxBatch: 10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.scheduledSendingRetry)
	require.Len(t, consumer.preSendRequeued, 1)
	require.NotNil(t, consumer.preSendRequeued[0].Retry)
	assert.Equal(t, 2, consumer.preSendRequeued[0].Retry.Attempt)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.releasedClaims)
}
