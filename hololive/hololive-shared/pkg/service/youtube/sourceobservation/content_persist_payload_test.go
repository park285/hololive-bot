package sourceobservation

import (
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/content"
)

func TestDomainNotificationCarriesPremiereSchedule(t *testing.T) {
	t.Parallel()

	scheduled := time.Date(2026, time.August, 24, 12, 30, 0, 0, time.UTC)
	isPremiere := true
	notification := domainNotification(&content.NotificationIntent{
		Kind:      domain.OutboxKindNewVideo,
		ChannelID: testChannelID,
		ContentID: testVideoID,
		Video: content.Entity{
			VideoID:      testVideoID,
			ChannelID:    testChannelID,
			Title:        "premiere",
			ScheduledFor: &scheduled,
			IsPremiere:   &isPremiere,
		},
	})

	var payload struct {
		VideoID          string     `json:"video_id"`
		ScheduledStartAt *time.Time `json:"scheduled_start_at"`
		IsPremiere       *bool      `json:"is_premiere"`
	}

	if err := jsonv2.Unmarshal([]byte(notification.Payload), &payload); err != nil {
		t.Fatalf("unmarshal notification payload: %v", err)
	}

	if payload.VideoID != testVideoID {
		t.Fatalf("video id = %q, want %q", payload.VideoID, testVideoID)
	}

	if payload.ScheduledStartAt == nil || !payload.ScheduledStartAt.Equal(scheduled) {
		t.Fatalf("scheduled_start_at = %v, want %s", payload.ScheduledStartAt, scheduled)
	}

	if payload.IsPremiere == nil || !*payload.IsPremiere {
		t.Fatalf("is_premiere = %v, want true", payload.IsPremiere)
	}
}
