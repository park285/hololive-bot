package alarmdispatch

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
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

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, []string{"주간 멤버 뉴스"}, sender.messages)
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

	groups := groupAlarmDispatchEnvelopesForDelivery(withAlarmDispatchTestSendUnitIdentity([]dispatchoutbox.AlarmQueueEnvelope{first, second}))
	require.Len(t, groups, 2)

	renderer, messageStrings := newAlarmDispatchTestRendering(t)
	seen := make(map[string]struct{}, len(groups))

	for i := range groups {
		message, handled, err := renderAlarmDispatchGroupSource(t.Context(), renderer, messageStrings, nil, groups[i])
		require.NoError(t, err)
		require.True(t, handled)
		// 변형 도입 전 digest는 한 본문만 담으므로 두 경로가 같은 본문을 받는다.
		require.Equal(t, message.Plain, message.RichText)

		seen[message.Plain] = struct{}{}
	}

	require.Contains(t, seen, "주간 멤버 뉴스 A")
	require.Contains(t, seen, "주간 멤버 뉴스 B")
}
