package notification

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestChannelSubscribersKeyByType(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	tests := []struct {
		name      string
		channelID string
		alarmType domain.AlarmType
		want      string
	}{
		{
			name:      "live uses default prefix",
			channelID: "UC_live",
			alarmType: domain.AlarmTypeLive,
			want:      ChannelSubscribersKeyPrefix + "UC_live",
		},
		{
			name:      "community uses dedicated prefix",
			channelID: "UC_community",
			alarmType: domain.AlarmTypeCommunity,
			want:      ChannelSubscribersCommunityPrefix + "UC_community",
		},
		{
			name:      "shorts uses dedicated prefix",
			channelID: "UC_shorts",
			alarmType: domain.AlarmTypeShorts,
			want:      ChannelSubscribersShortsPrefix + "UC_shorts",
		},
		{
			name:      "unknown type falls back to default",
			channelID: "UC_unknown",
			alarmType: domain.AlarmType("UNKNOWN"),
			want:      ChannelSubscribersKeyPrefix + "UC_unknown",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := as.channelSubscribersKeyByType(tt.channelID, tt.alarmType); got != tt.want {
				t.Fatalf("channelSubscribersKeyByType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildTitleFingerprint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		streamID string
		wantLen  int
	}{
		{name: "uses title", title: "페코라 방송", streamID: "vid1", wantLen: 16},
		{name: "falls back to stream id", title: "", streamID: "vid2", wantLen: 16},
		{name: "falls back to untitled", title: "", streamID: "", wantLen: 16},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildTitleFingerprint(tt.title, tt.streamID)
			if len(got) != tt.wantLen {
				t.Fatalf("buildTitleFingerprint() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}

	if got1, got2 := buildTitleFingerprint("같은 제목", "a"), buildTitleFingerprint("같은 제목", "b"); got1 != got2 {
		t.Fatalf("expected same fingerprint for same normalized title, got %q and %q", got1, got2)
	}
	if got1, got2 := buildTitleFingerprint("", "stream-a"), buildTitleFingerprint("", "stream-b"); got1 == got2 {
		t.Fatalf("expected different fingerprints for different stream id fallback, got same %q", got1)
	}
}

func TestResolveStreamChannelID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		stream    *domain.Stream
		fallback  string
		wantValue string
	}{
		{
			name:      "nil stream",
			stream:    nil,
			fallback:  "default",
			wantValue: "default",
		},
		{
			name:      "stream channel id has priority",
			stream:    &domain.Stream{ChannelID: "stream-id", Channel: &domain.Channel{ID: "channel-id"}},
			fallback:  "default",
			wantValue: "stream-id",
		},
		{
			name:      "falls back to stream channel object",
			stream:    &domain.Stream{Channel: &domain.Channel{ID: "channel-id"}},
			fallback:  "default",
			wantValue: "channel-id",
		},
		{
			name:      "falls back to default",
			stream:    &domain.Stream{},
			fallback:  "default",
			wantValue: "default",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveStreamChannelID(tt.stream, tt.fallback); got != tt.wantValue {
				t.Fatalf("resolveStreamChannelID() = %q, want %q", got, tt.wantValue)
			}
		})
	}
}

func TestBuildUpcomingEventKey(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	start := time.Date(2026, 3, 2, 10, 30, 59, 0, time.UTC)
	key := as.buildUpcomingEventKey("room1", "channel1", "stream1", "Title", start)

	parts := strings.Split(key, ":")
	if len(parts) < 7 {
		t.Fatalf("unexpected key format: %q", key)
	}

	if !strings.HasPrefix(key, UpcomingEventKeyPrefix+"room1:channel1:") {
		t.Fatalf("unexpected prefix: %q", key)
	}

	wantUnix := normalizeScheduledMinute(start).Unix()
	if got := parts[len(parts)-2]; got != fmt.Sprintf("%d", wantUnix) {
		t.Fatalf("scheduled minute unix mismatch: got=%s want=%d", got, wantUnix)
	}

	if gotFingerprint := parts[len(parts)-1]; gotFingerprint != buildTitleFingerprint("Title", "stream1") {
		t.Fatalf("unexpected fingerprint: got=%s want=%s", gotFingerprint, buildTitleFingerprint("Title", "stream1"))
	}
}

func TestMarkUpcomingEventNotifiedAndWasRecently(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(30 * time.Minute)

	stream := &domain.Stream{
		ID:             "stream1",
		Title:          "테스트 방송",
		StartScheduled: &start,
	}

	if err := as.MarkUpcomingEventNotified(ctx, "room1", "channel1", stream); err != nil {
		t.Fatalf("MarkUpcomingEventNotified() error = %v", err)
	}

	if !as.WasUpcomingEventNotifiedRecently(ctx, "room1", "channel1", stream, time.Hour) {
		t.Fatalf("expected upcoming event to be considered recently notified")
	}

	if as.WasUpcomingEventNotifiedRecently(ctx, "room1", "channel1", stream, 0) {
		t.Fatalf("expected zero window to return false")
	}

	if as.WasUpcomingEventNotifiedRecently(ctx, "other-room", "channel1", stream, time.Hour) {
		t.Fatalf("expected different room key to return false")
	}

	key := as.buildUpcomingEventKey("room1", "channel1", stream.ID, stream.Title, *stream.StartScheduled)
	if err := as.cache.Set(ctx, key, UpcomingEventNotifiedData{NotifiedAt: "invalid-time"}, constants.CacheTTL.NotificationSent); err != nil {
		t.Fatalf("cache.Set invalid payload failed: %v", err)
	}
	if as.WasUpcomingEventNotifiedRecently(ctx, "room1", "channel1", stream, time.Hour) {
		t.Fatalf("expected invalid notified timestamp to return false")
	}

	if err := as.MarkUpcomingEventNotified(ctx, "room1", "channel1", nil); err != nil {
		t.Fatalf("MarkUpcomingEventNotified(nil) error = %v", err)
	}
}

func TestGetNextStreamInfo(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()
	channelID := "UC_test"
	key := NextStreamKeyPrefix + channelID

	tests := []struct {
		name   string
		seed   map[string]any
		assert func(t *testing.T, got *domain.NextStreamInfo, err error)
	}{
		{
			name: "missing cache returns nil",
			seed: nil,
			assert: func(t *testing.T, got *domain.NextStreamInfo, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != nil {
					t.Fatalf("expected nil info, got %#v", got)
				}
			},
		},
		{
			name: "invalid status ignored",
			seed: map[string]any{"status": "broken", "video_id": "v1", "title": "t1"},
			assert: func(t *testing.T, got *domain.NextStreamInfo, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != nil {
					t.Fatalf("expected nil info for invalid status, got %#v", got)
				}
			},
		},
		{
			name: "upcoming requires complete fields",
			seed: map[string]any{"status": "upcoming", "video_id": "v1"},
			assert: func(t *testing.T, got *domain.NextStreamInfo, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != nil {
					t.Fatalf("expected nil info for incomplete upcoming, got %#v", got)
				}
			},
		},
		{
			name: "upcoming complete fields",
			seed: map[string]any{
				"status":          "upcoming",
				"video_id":        "v1",
				"title":           "테스트",
				"start_scheduled": "2026-03-02T00:00:00Z",
			},
			assert: func(t *testing.T, got *domain.NextStreamInfo, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got == nil {
					t.Fatalf("expected info, got nil")
				}
				if got.Status != domain.NextStreamStatusUpcoming || got.VideoID != "v1" || got.Title != "테스트" {
					t.Fatalf("unexpected info: %#v", got)
				}
				if got.StartScheduled == nil || got.StartScheduled.Format(time.RFC3339) != "2026-03-02T00:00:00Z" {
					t.Fatalf("unexpected scheduled time: %#v", got.StartScheduled)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if err := as.cache.Del(ctx, key); err != nil {
				t.Fatalf("cache delete failed: %v", err)
			}

			if len(tt.seed) > 0 {
				fields := make(map[string]any, len(tt.seed))
				for k, v := range tt.seed {
					fields[k] = v
				}
				if err := as.cache.HMSet(ctx, key, fields); err != nil {
					t.Fatalf("cache HMSet failed: %v", err)
				}
			}

			got, err := as.GetNextStreamInfo(ctx, channelID)
			tt.assert(t, got, err)
		})
	}
}

func TestNormalizeScheduledMinute(t *testing.T) {
	t.Parallel()

	input := time.Date(2026, 3, 2, 5, 4, 59, 123, time.UTC)
	want := time.Date(2026, 3, 2, 5, 4, 0, 0, time.UTC)
	if got := normalizeScheduledMinute(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeScheduledMinute() = %v, want %v", got, want)
	}
}
