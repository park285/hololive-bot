package workerapp

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAlarmDispatchRunnerRetryable502AfterMarkSendingUsesScheduleSendingRetry는
// PG 모드에서 row가 'sending' 상태일 때 502 에러가 발생하면 ScheduleSendingRetry를
// 호출해야 하며 ScheduleRetry (status='leased' 조건) 는 호출되지 않아야 함을 검증한다.
func TestAlarmDispatchRunnerRetryable502AfterMarkSendingUsesScheduleSendingRetry(t *testing.T) {
	// 실제 HTTP 에러 타입을 사용 — URL이 /karing/content-list여도 아니어도 502면 retryable
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
	// MarkSending은 호출됨 (row가 sending 상태로 전환)
	require.Len(t, consumer.markSending, 1)
	// ScheduleSendingRetry가 호출되어 sending → retry 전환
	require.Len(t, consumer.scheduledSendingRetry, 1, "502 post-send failure must route through ScheduleSendingRetry, not ScheduleRetry")
	require.NotNil(t, consumer.scheduledSendingRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledSendingRetry[0].Retry.Attempt)
	// ScheduleRetry (status='leased' 조건) 는 호출되지 않아야 함
	assert.Empty(t, consumer.scheduledRetry, "ScheduleRetry must not be called for post-send failure when row is already 'sending'")
	// quarantine도 발생하지 않아야 함
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

// TestAlarmDispatchRunnerRetryable503AfterMarkSendingUsesScheduleSendingRetry는
// 503 에러에 대해서도 동일하게 ScheduleSendingRetry 경로를 사용함을 검증한다.
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

// alarmDispatchRunnerSendingRetryTestConsumer는 alarmDispatchConsumer와
// alarmDispatchSendingRetryConsumer 두 인터페이스를 모두 구현하는 테스트용 mock이다.
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
