package dispatchrun

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
)

func TestAlarmDispatchRunnerSendsPreRenderedDeliveryDigest(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.Notification.AlarmType = domain.AlarmTypeCommunity
	envelope.SourceKind = domain.AlarmDispatchSourceKindDeliveryDigest
	envelope.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{
		Kind:               domain.DeliveryKindMemberNewsWeekly,
		PeriodKey:          "2026-W32",
		PreRenderedMessage: "주간 멤버 뉴스",
	}
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, karingEnabled: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, []string{"주간 멤버 뉴스"}, sender.messages)
	require.Empty(t, sender.karingRequests)
	require.Len(t, consumer.markDispatched, 1)
}
