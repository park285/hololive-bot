package dispatchrun

import (
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errAlarmDispatchRunnerTestMarkSending = errors.New("mark sending partial update")

func TestAlarmDispatchRunnerCompensatesMarkSendingFailureWithSendingRetry(t *testing.T) {
	consumer := &alarmDispatchRunnerSendingRetryTestConsumer{
		batches:        [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}},
		markSendingErr: errAlarmDispatchRunnerTestMarkSending,
	}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{
		consumer:           consumer,
		sender:             sender,
		renderer:           newAlarmDispatchTestRenderer(t),
		postSendQuarantine: true,
		maxBatch:           10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, sender.messages, "메시지는 발송되면 안 된다")
	require.Len(t, consumer.scheduledSendingRetry, 1,
		"MarkSending 실패는 이미 커밋된 sending 행을 복원할 수 있는 ScheduleSendingRetry로 보상해야 한다")
	require.NotNil(t, consumer.scheduledSendingRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledSendingRetry[0].Retry.Attempt)
	assert.Contains(t, consumer.scheduledSendingRetry[0].Retry.LastError, errAlarmDispatchRunnerTestMarkSending.Error())
	assert.Empty(t, consumer.scheduledRetry, "leased 전용 ScheduleRetry로 보상하면 sending 행이 잔류한다")
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerCompensatesKaringMarkSendingFailureWithSendingRetry(t *testing.T) {
	consumer := &alarmDispatchRunnerSendingRetryTestConsumer{
		batches:        [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}},
		markSendingErr: errAlarmDispatchRunnerTestMarkSending,
	}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{
		consumer:           consumer,
		sender:             sender,
		karingEnabled:      true,
		postSendQuarantine: true,
		maxBatch:           10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, sender.karingRequests, "karing 요청은 발송되면 안 된다")
	require.Len(t, consumer.scheduledSendingRetry, 1)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerMarkSendingFailureMovesExhaustedEnvelopeToDLQ(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", &domain.AlarmQueueRetryMetadata{Attempt: 2})
	envelope.ClaimKeys = []string{"alarm:dispatch:claim:room-1:stream-1"}
	consumer := &alarmDispatchRunnerSendingRetryTestConsumer{
		batches:        [][]domain.AlarmQueueEnvelope{{envelope}},
		markSendingErr: errAlarmDispatchRunnerTestMarkSending,
	}
	runner := Runner{
		consumer:           consumer,
		sender:             &alarmDispatchRunnerTestSender{},
		renderer:           newAlarmDispatchTestRenderer(t),
		postSendQuarantine: true,
		maxBatch:           10,
	}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.scheduledSendingRetry)
	require.Len(t, consumer.movedDLQ, 1)
	require.NotNil(t, consumer.movedDLQ[0].Retry)
	assert.Equal(t, 3, consumer.movedDLQ[0].Retry.Attempt)
	assert.Equal(t, []string{"alarm:dispatch:claim:room-1:stream-1"}, consumer.releasedClaims)
}

func TestAlarmDispatchRunnerMarkSendingFailureWithoutSendingRetryConsumerReturnsError(t *testing.T) {
	consumer := &alarmDispatchRunnerLegacyTestConsumer{
		batches:        [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}},
		markSendingErr: errAlarmDispatchRunnerTestMarkSending,
	}
	runner := Runner{
		consumer: consumer,
		sender:   &alarmDispatchRunnerTestSender{},
		renderer: newAlarmDispatchTestRenderer(t),
		maxBatch: 10,
	}

	processed, err := runner.runOnce(t.Context())

	require.Error(t, err)
	assert.True(t, processed)
	assert.ErrorIs(t, err, errAlarmDispatchRunnerTestMarkSending)
	assert.Empty(t, consumer.scheduledRetry,
		"ScheduleSendingRetry 없는 소비자는 leased 전용 ScheduleRetry로 잘못 보상하지 않고 에러를 그대로 반환해야 한다")
	assert.Empty(t, consumer.movedDLQ)
}
