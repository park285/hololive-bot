package domain

import (
	"strings"
	"testing"
)

func TestAlarmNotificationValidateLiveDispatchPersistenceIdentity(t *testing.T) {
	t.Parallel()

	valid := func() *AlarmNotification {
		return &AlarmNotification{
			AlarmType: AlarmTypeLive,
			RoomID:    strings.Repeat("r", maxAlarmRoomIDBytes),
			Channel:   &Channel{ID: "UC_channel"},
			Stream:    &Stream{ID: "stream-1", ChannelID: "UC_channel"},
		}
	}
	if err := valid().ValidateLiveDispatchPersistenceIdentity(); err != nil {
		t.Fatalf("valid identity error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AlarmNotification)
	}{
		{name: "empty room", mutate: func(n *AlarmNotification) { n.RoomID = "" }},
		{name: "overlong room", mutate: func(n *AlarmNotification) { n.RoomID = strings.Repeat("r", maxAlarmRoomIDBytes+1) }},
		{name: "overlong stream", mutate: func(n *AlarmNotification) { n.Stream.ID = strings.Repeat("s", maxAlarmStreamIDBytes+1) }},
		{name: "overlong channel", mutate: func(n *AlarmNotification) {
			n.Channel.ID = strings.Repeat("c", maxAlarmChannelIDBytes+1)
			n.Stream.ChannelID = n.Channel.ID
		}},
		{name: "ambiguous channel", mutate: func(n *AlarmNotification) { n.Stream.ChannelID = "UC_other" }},
		{name: "surrounding whitespace", mutate: func(n *AlarmNotification) { n.Stream.ID = " stream-1 " }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			notification := valid()
			test.mutate(notification)
			if err := notification.ValidateLiveDispatchPersistenceIdentity(); err == nil {
				t.Fatal("ValidateLiveDispatchPersistenceIdentity() error = nil")
			}
		})
	}
}
