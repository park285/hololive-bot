package keys

import (
	"testing"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestIsRoomAlarmKeySeparatesRoomKeysFromReservedNamespaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "room", key: "alarm:room-1", want: true},
		{name: "registry", key: AlarmRegistryKey, want: false},
		{name: "dispatch queue", key: contractsalarm.DispatchQueueKey, want: false},
		{name: "channel subscriber", key: ChannelSubscribersKeyPrefix + "UC_TEST", want: false},
		{name: "empty suffix", key: AlarmKeyPrefix, want: false},
		{name: "other namespace", key: "notified:stream-1", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsRoomAlarmKey(tt.key); got != tt.want {
				t.Fatalf("IsRoomAlarmKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}
