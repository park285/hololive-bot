package alarmdispatch

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

func TestGroupAlarmDispatchEnvelopesForDelivery(t *testing.T) {
	t.Parallel()

	sendUnitEnvelope := func(sendUnitID int64, minutesUntil int) dispatchoutbox.AlarmQueueEnvelope {
		envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

		envelope.SendUnitID = sendUnitID
		envelope.Notification.MinutesUntil = minutesUntil

		return envelope
	}
	undeliverable := sendUnitEnvelope(7, 5)

	undeliverable.Notification.Stream = nil

	testCases := []struct {
		name      string
		envelopes []dispatchoutbox.AlarmQueueEnvelope
		want      []alarmDispatchGroupSummary
	}{
		{
			name:      "groups envelopes by persisted send unit",
			envelopes: []dispatchoutbox.AlarmQueueEnvelope{sendUnitEnvelope(7, 5), sendUnitEnvelope(8, 5), sendUnitEnvelope(7, 3)},
			want: []alarmDispatchGroupSummary{
				{roomID: testAlarmRoomID, minutesUntil: 3, envelopeCount: 2, notificationCount: 2},
				{roomID: testAlarmRoomID, minutesUntil: 5, envelopeCount: 1, notificationCount: 1},
			},
		},
		{
			name:      "keeps undeliverable envelopes apart from the same send unit",
			envelopes: []dispatchoutbox.AlarmQueueEnvelope{sendUnitEnvelope(7, 5), undeliverable, sendUnitEnvelope(7, 5)},
			want: []alarmDispatchGroupSummary{
				{roomID: testAlarmRoomID, minutesUntil: 5, envelopeCount: 2, notificationCount: 2},
				{roomID: testAlarmRoomID, minutesUntil: 5, envelopeCount: 1, notificationCount: 1, undeliverable: true},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			groups := groupAlarmDispatchEnvelopesForDelivery(tc.envelopes)

			assert.Equal(t, tc.want, summarizeAlarmDispatchGroups(groups))
		})
	}
}

func TestMinAlarmDispatchMinutes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		current int
		next    int
		want    int
	}{
		{name: "negative next is ignored", current: 10, next: -1, want: 10},
		{name: "negative current is replaced", current: -1, next: 5, want: 5},
		{name: "current smaller is kept", current: 3, next: 10, want: 3},
		{name: "next smaller is used", current: 10, next: 3, want: 3},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, minAlarmDispatchMinutes(tc.current, tc.next))
		})
	}
}

type alarmDispatchGroupSummary struct {
	roomID            string
	minutesUntil      int
	envelopeCount     int
	notificationCount int
	undeliverable     bool
}

func summarizeAlarmDispatchGroups(groups []alarmDispatchGroup) []alarmDispatchGroupSummary {
	summaries := make([]alarmDispatchGroupSummary, 0, len(groups))
	for _, group := range groups {
		summaries = append(summaries, alarmDispatchGroupSummary{
			roomID:            group.roomID,
			minutesUntil:      group.minutesUntil,
			envelopeCount:     len(group.envelopes),
			notificationCount: len(group.notifications),
			undeliverable:     group.envelopeErr != nil,
		})
	}

	return summaries
}
