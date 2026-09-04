package dispatchrun

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmDispatchRunnerSendsPreRenderedDeliveryDigest(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.AlarmType = domain.AlarmTypeCommunity
	envelope.SourceKind = domain.AlarmDispatchSourceKindDeliveryDigest
	envelope.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{
		Kind:               domain.DeliveryKindMemberNewsWeekly,
		PeriodKey:          "2026-W32",
		PreRenderedMessage: "주간 멤버 뉴스",
	}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{consumer: consumer, sender: sender, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, []string{"주간 멤버 뉴스"}, sender.messages)
	require.Empty(t, sender.karingRequests)
	require.Len(t, consumer.markDispatched, 1)
}

func TestDeliveryDigestGroupingAndRenderingUsesContentIdentity(t *testing.T) {
	t.Parallel()

	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.Notification.AlarmType = domain.AlarmTypeCommunity
	first.SourceKind = domain.AlarmDispatchSourceKindDeliveryDigest
	first.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{
		Kind:               domain.DeliveryKindMemberNewsWeekly,
		PeriodKey:          "2026-W32",
		PreRenderedMessage: "주간 멤버 뉴스 A",
	}

	second := first

	second.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{
		Kind:               domain.DeliveryKindMemberNewsWeekly,
		PeriodKey:          "2026-W32",
		PreRenderedMessage: "주간 멤버 뉴스 B",
	}

	groups := groupAlarmDispatchEnvelopesForDelivery(t.Context(), &alarmDispatchRunnerTestSender{}, []domain.AlarmQueueEnvelope{first, second})
	require.Len(t, groups, 2)

	renderer, messageStrings := newAlarmDispatchTestRendering(t)
	seen := make(map[string]struct{}, len(groups))

	for i := range groups {
		message, handled, err := renderAlarmDispatchGroupSource(t.Context(), renderer, messageStrings, groups[i])
		require.NoError(t, err)
		require.True(t, handled)

		seen[message] = struct{}{}
	}

	require.Contains(t, seen, "주간 멤버 뉴스 A")
	require.Contains(t, seen, "주간 멤버 뉴스 B")
}
