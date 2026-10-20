package alarmdispatch

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 발송 진입 미스는 외부 호출 없이 원인으로만 갈린다. 멤버 종단만 격리하고, 멤버십 변경은 반환하며, 소유권을 잃은
// 미스는 행을 현재 소유자와 회수 경로에 남긴다.
func TestAlarmDispatchRunnerRoutesPreSendMissByReason(t *testing.T) {
	cases := []struct {
		name           string
		reasons        []sendfence.Reason
		wantQuarantine bool
		wantRelease    bool
	}{
		{name: "member terminal", reasons: []sendfence.Reason{sendfence.ReasonMemberTerminal}, wantQuarantine: true},
		{name: "membership changed", reasons: []sendfence.Reason{sendfence.ReasonMembershipChanged}, wantRelease: true},
		{name: "membership with peer", reasons: []sendfence.Reason{sendfence.ReasonMembershipChanged, sendfence.ReasonPeerMissed}, wantRelease: true},
		{name: "lease expired", reasons: []sendfence.Reason{sendfence.ReasonLeaseExpired}},
		{name: "owner changed beside terminal", reasons: []sendfence.Reason{sendfence.ReasonOwnerChanged, sendfence.ReasonMemberTerminal}},
		{name: "request changed", reasons: []sendfence.Reason{sendfence.ReasonRequestChanged}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envelopes := make([]domain.AlarmQueueEnvelope, len(tc.reasons))
			miss := &sendfence.MissError{Action: "begin_send_request", Expected: len(tc.reasons)}

			for i, reason := range tc.reasons {
				envelopes[i] = alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
				envelopes[i].DispatchOutboxID = int64(i + 1)
				miss.Rows = append(miss.Rows, sendfence.RowMiss{ID: int64(i + 1), Reason: reason, Status: "leased"})
			}

			consumer := &alarmDispatchRunnerTestConsumer{markSendingErr: miss}
			sender := &alarmDispatchRunnerTestSender{}
			runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender}

			proceed, err := runner.markSending(t.Context(), alarmDispatchGroup{roomID: testAlarmRoomID, envelopes: envelopes})

			require.NoError(t, err)
			require.False(t, proceed)
			require.Empty(t, sender.messages)
			require.Empty(t, consumer.preSendRequeued, "a fence miss is not a same-attempt requeue")
			require.Equal(t, tc.wantQuarantine, len(consumer.quarantined) == len(envelopes))
			require.Equal(t, tc.wantRelease, len(consumer.released) == len(envelopes))

			if !tc.wantQuarantine {
				require.Empty(t, consumer.quarantined)
			}

			if !tc.wantRelease {
				require.Empty(t, consumer.released)
			}
		})
	}
}
