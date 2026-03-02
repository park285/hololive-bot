package notification

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildQueueGroupKey(t *testing.T) {
	t.Parallel()

	scheduled := time.Date(2026, 3, 2, 12, 30, 45, 0, time.UTC)
	tests := []struct {
		name string
		n    *domain.AlarmNotification
		want string
	}{
		{
			name: "nil notification",
			n:    nil,
			want: "",
		},
		{
			name: "scheduled stream groups by minute",
			n: &domain.AlarmNotification{
				RoomID: "room1",
				Stream: &domain.Stream{StartScheduled: &scheduled},
			},
			want: fmt.Sprintf("room1|scheduled|%d", scheduled.Truncate(time.Minute).Unix()),
		},
		{
			name: "fallback groups by minutes until",
			n: &domain.AlarmNotification{
				RoomID:       "room1",
				MinutesUntil: 5,
			},
			want: "room1|minutes|5",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := buildQueueGroupKey(tt.n); got != tt.want {
				t.Fatalf("buildQueueGroupKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGroupQueueNotifications(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 3, 2, 8, 0, 10, 0, time.UTC)
	t1 := time.Date(2026, 3, 2, 8, 0, 50, 0, time.UTC) // 같은 분
	notifications := []*domain.AlarmNotification{
		nil,
		{RoomID: "room1", MinutesUntil: 5, Stream: &domain.Stream{ID: "s1", StartScheduled: &t0}},
		{RoomID: "room1", MinutesUntil: 3, Stream: &domain.Stream{ID: "s2", StartScheduled: &t1}},
		{RoomID: "room1", MinutesUntil: 9, Stream: &domain.Stream{ID: "s3"}},
		{RoomID: "room2", MinutesUntil: 1, Stream: &domain.Stream{ID: "s4", StartScheduled: &t0}},
	}

	groups := groupQueueNotifications(notifications)
	if len(groups) != 3 {
		t.Fatalf("group count = %d, want 3", len(groups))
	}

	if groups[0].roomID != "room1" || groups[0].minutesUntil != 3 || len(groups[0].notifications) != 2 {
		t.Fatalf("unexpected first group: %#v", groups[0])
	}
	if groups[1].roomID != "room1" || groups[1].minutesUntil != 9 || len(groups[1].notifications) != 1 {
		t.Fatalf("unexpected second group: %#v", groups[1])
	}
	if groups[2].roomID != "room2" || groups[2].minutesUntil != 1 || len(groups[2].notifications) != 1 {
		t.Fatalf("unexpected third group: %#v", groups[2])
	}
}

func TestCollectClaimKeys(t *testing.T) {
	t.Parallel()

	envelopes := []*domain.AlarmQueueEnvelope{
		{
			Notification: domain.AlarmNotification{RoomID: "room1", Stream: &domain.Stream{ID: "s1"}},
			ClaimKeys:    []string{"notified:claim:a", "notified:claim:b"},
		},
		{
			Notification: domain.AlarmNotification{RoomID: "room1", Stream: &domain.Stream{ID: "s2"}},
			ClaimKeys:    []string{"notified:claim:c"},
		},
		{
			Notification: domain.AlarmNotification{RoomID: "room2", Stream: &domain.Stream{ID: "s3"}},
			ClaimKeys:    []string{"notified:claim:d"},
		},
	}
	group := &queueNotificationGroup{
		roomID: "room1",
		notifications: []*domain.AlarmNotification{
			{RoomID: "room1", Stream: &domain.Stream{ID: "s1"}},
			{RoomID: "room1", Stream: &domain.Stream{ID: "s2"}},
			{RoomID: "room1", Stream: nil}, // should be ignored
		},
	}

	got := collectClaimKeys(envelopes, group)
	want := []string{"notified:claim:a", "notified:claim:b", "notified:claim:c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectClaimKeys() = %#v, want %#v", got, want)
	}
}

func TestResolveQueueNotificationChannelID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		n    *domain.AlarmNotification
		want string
	}{
		{
			name: "nil notification",
			n:    nil,
			want: "",
		},
		{
			name: "channel id has priority",
			n: &domain.AlarmNotification{
				Channel: &domain.Channel{ID: "channel-id"},
				Stream:  &domain.Stream{ChannelID: "stream-channel"},
			},
			want: "channel-id",
		},
		{
			name: "falls back to stream channel id",
			n: &domain.AlarmNotification{
				Stream: &domain.Stream{ChannelID: "stream-channel"},
			},
			want: "stream-channel",
		},
		{
			name: "missing channel and stream",
			n:    &domain.AlarmNotification{},
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveQueueNotificationChannelID(tt.n); got != tt.want {
				t.Fatalf("resolveQueueNotificationChannelID() = %q, want %q", got, tt.want)
			}
		})
	}
}
