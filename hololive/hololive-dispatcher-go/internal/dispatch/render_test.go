package dispatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestSimpleRenderer_RenderGroupSingle(t *testing.T) {
	t.Parallel()

	renderer := NewSimpleRenderer()
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	message, err := renderer.RenderGroup(context.Background(), NotificationGroup{
		RoomID:       "room-1",
		MinutesUntil: 5,
		Notifications: []domain.AlarmNotification{
			{
				RoomID:  "room-1",
				Channel: &domain.Channel{ID: "channel-id", Name: "시온"},
				Stream: &domain.Stream{
					ID:             "stream-1",
					Title:          "테스트 방송",
					ChannelID:      "channel-id",
					ChannelName:    "시온",
					Status:         domain.StreamStatusUpcoming,
					StartScheduled: &start,
				},
				MinutesUntil: 5,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderGroup() error = %v", err)
	}

	if !strings.Contains(message, "시온") {
		t.Fatalf("expected member name in message, got %q", message)
	}
	if !strings.Contains(message, "테스트 방송") {
		t.Fatalf("expected title in message, got %q", message)
	}
}

func TestSimpleRenderer_RenderGroupMultiple(t *testing.T) {
	t.Parallel()

	renderer := NewSimpleRenderer()
	message, err := renderer.RenderGroup(context.Background(), NotificationGroup{
		RoomID:       "room-1",
		MinutesUntil: 3,
		Notifications: []domain.AlarmNotification{
			{
				RoomID:       "room-1",
				Channel:      &domain.Channel{ID: "c1", Name: "멤버1"},
				Stream:       &domain.Stream{ID: "s1", Title: "방송1", ChannelName: "멤버1"},
				MinutesUntil: 3,
			},
			{
				RoomID:       "room-1",
				Channel:      &domain.Channel{ID: "c2", Name: "멤버2"},
				Stream:       &domain.Stream{ID: "s2", Title: "방송2", ChannelName: "멤버2"},
				MinutesUntil: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderGroup() error = %v", err)
	}

	if !strings.Contains(message, "3분 내 방송 알림") {
		t.Fatalf("expected grouped header, got %q", message)
	}
	if !strings.Contains(message, "멤버1") || !strings.Contains(message, "멤버2") {
		t.Fatalf("expected both member names, got %q", message)
	}
}
