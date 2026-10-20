package alarmdispatch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// alarmDispatchOrderConsumer는 claim과 정산 호출 순서를 기록한다.
type alarmDispatchOrderConsumer struct {
	*alarmDispatchRunnerTestConsumer

	ops []string
}

func (c *alarmDispatchOrderConsumer) DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
	c.ops = append(c.ops, "drain")

	return c.alarmDispatchRunnerTestConsumer.DrainBatch(ctx, maxItems)
}

func (c *alarmDispatchOrderConsumer) MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.ops = append(c.ops, "mark-sending")

	return c.alarmDispatchRunnerTestConsumer.MarkSending(ctx, envelopes)
}

func (c *alarmDispatchOrderConsumer) MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, irisRequestID string, handoffHorizon time.Duration) error {
	c.ops = append(c.ops, "mark-sent")

	return c.alarmDispatchRunnerTestConsumer.MarkDispatched(ctx, envelopes, irisRequestID, handoffHorizon)
}

// 같은 worker ID가 같은 attempt의 행을 다시 claim하지 않는 근거는 직렬 루프다. 한 배치의 모든 그룹이 정산된 뒤에만
// 다음 claim을 한다.
func TestAlarmDispatchRunnerClaimsNextBatchOnlyAfterSettlingPrevious(t *testing.T) {
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope("room-2", nil)
	consumer := &alarmDispatchOrderConsumer{alarmDispatchRunnerTestConsumer: &alarmDispatchRunnerTestConsumer{
		batches: [][]domain.AlarmQueueEnvelope{{first, second}, {first}},
	}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	for range 2 {
		processed, err := runner.runOnce(t.Context())
		require.NoError(t, err)
		require.True(t, processed)
	}

	require.Equal(t, []string{
		"drain", "mark-sending", "mark-sent", "mark-sending", "mark-sent",
		"drain", "mark-sending", "mark-sent",
	}, consumer.ops)
}
