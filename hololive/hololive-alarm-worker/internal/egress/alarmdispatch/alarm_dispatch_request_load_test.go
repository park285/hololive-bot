package alarmdispatch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type loadCountingConsumer struct {
	*alarmDispatchRunnerTestConsumer

	loads int
}

func (c *loadCountingConsumer) LoadSendRequest(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (*dispatchoutbox.SendRequest, error) {
	c.loads++

	return c.alarmDispatchRunnerTestConsumer.LoadSendRequest(ctx, envelopes)
}

func TestAlarmDispatchRunnerLoadsRequestOnlyWhenClaimDidNotSeeItUnpinned(t *testing.T) {
	for _, item := range []struct {
		name     string
		unpinned bool
		loads    int
	}{
		{name: "unpinned at claim pins directly", unpinned: true, loads: 0},
		{name: "unknown at claim loads first", unpinned: false, loads: 1},
	} {
		t.Run(item.name, func(t *testing.T) {
			envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

			envelope.RequestUnpinned = item.unpinned

			consumer := &loadCountingConsumer{alarmDispatchRunnerTestConsumer: &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}}
			sender := &alarmDispatchRunnerTestSender{}
			runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

			processed, err := runner.runOnce(t.Context())

			require.NoError(t, err)
			require.True(t, processed)
			require.Equal(t, item.loads, consumer.loads)
			require.Len(t, consumer.requests, 1)
			require.Len(t, sender.messages, 1)
			require.Len(t, consumer.markDispatched, 1)
		})
	}
}
