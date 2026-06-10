package alarmcache

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
)

func newPureState() *State {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewState(nil, nil, logger)
}

func TestNormalizeScheduledMinute(t *testing.T) {
	t.Parallel()

	in := time.Date(2026, 6, 10, 12, 34, 56, 789, time.UTC)
	got := NormalizeScheduledMinute(in)
	want := time.Date(2026, 6, 10, 12, 34, 0, 0, time.UTC)
	require.True(t, got.Equal(want), "got %s want %s", got, want)
}

func TestNotifiedMinuteKey(t *testing.T) {
	t.Parallel()

	scheduled := time.Date(2026, 6, 10, 12, 34, 56, 0, time.UTC)
	normalizedUnix := time.Date(2026, 6, 10, 12, 34, 0, 0, time.UTC).Unix()

	tests := []struct {
		name         string
		streamID     string
		minutesUntil int
		want         string
	}{
		{
			name:         "trimmed stream id",
			streamID:     "  vid42  ",
			minutesUntil: 5,
			want:         fmt.Sprintf("%svid42:%d:%d", NotifiedKeyPrefix, normalizedUnix, 5),
		},
		{
			name:         "empty stream id",
			streamID:     "",
			minutesUntil: 30,
			want:         fmt.Sprintf("%s:%d:%d", NotifiedKeyPrefix, normalizedUnix, 30),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, NotifiedMinuteKey(tt.streamID, scheduled, tt.minutesUntil))
		})
	}
}

func TestBuildUpcomingEventKey(t *testing.T) {
	t.Parallel()

	state := newPureState()
	scheduled := time.Date(2026, 6, 10, 12, 34, 56, 0, time.UTC)
	scheduledUnix := time.Date(2026, 6, 10, 12, 34, 0, 0, time.UTC).Unix()
	fingerprint := BuildTitleFingerprint("My Title", "vid1")

	got := state.BuildUpcomingEventKey("room1", "UC_alpha", "vid1", "My Title", scheduled)
	want := fmt.Sprintf("%sroom1:UC_alpha:%d:%s", UpcomingEventKeyPrefix, scheduledUnix, fingerprint)
	require.Equal(t, want, got)
}

func TestResolveStreamChannelID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stream   *domain.Stream
		fallback string
		want     string
	}{
		{
			name:     "nil stream uses fallback",
			stream:   nil,
			fallback: "UC_fallback",
			want:     "UC_fallback",
		},
		{
			name:     "stream channel id wins",
			stream:   &domain.Stream{ChannelID: " UC_direct "},
			fallback: "UC_fallback",
			want:     "UC_direct",
		},
		{
			name:     "nested channel id used when direct blank",
			stream:   &domain.Stream{ChannelID: "   ", Channel: &domain.Channel{ID: " UC_nested "}},
			fallback: "UC_fallback",
			want:     "UC_nested",
		},
		{
			name:     "fallback when both blank",
			stream:   &domain.Stream{ChannelID: "  ", Channel: &domain.Channel{ID: "  "}},
			fallback: "UC_fallback",
			want:     "UC_fallback",
		},
		{
			name:     "fallback when channel nil and direct blank",
			stream:   &domain.Stream{ChannelID: ""},
			fallback: "UC_fallback",
			want:     "UC_fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ResolveStreamChannelID(tt.stream, tt.fallback))
		})
	}
}

func TestFirstMemberName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []string
		want       string
	}{
		{name: "first non-blank wins", candidates: []string{"  ", "  short ", "ko"}, want: "short"},
		{name: "skips blanks", candidates: []string{"", "\t", "name"}, want: "name"},
		{name: "all blank", candidates: []string{"", "   "}, want: ""},
		{name: "no candidates", candidates: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, FirstMemberName(tt.candidates...))
		})
	}
}

func TestParseNextStreamInfo(t *testing.T) {
	t.Parallel()

	state := newPureState()
	scheduledStr := "2026-06-10T12:34:56Z"
	scheduledTime, err := time.Parse(time.RFC3339, scheduledStr)
	require.NoError(t, err)

	tests := []struct {
		name     string
		data     map[string]string
		assertFn func(t *testing.T, info *domain.NextStreamInfo)
	}{
		{
			name: "empty data returns nil",
			data: map[string]string{},
			assertFn: func(t *testing.T, info *domain.NextStreamInfo) {
				require.Nil(t, info)
			},
		},
		{
			name: "invalid status returns nil",
			data: map[string]string{"status": "garbage"},
			assertFn: func(t *testing.T, info *domain.NextStreamInfo) {
				require.Nil(t, info)
			},
		},
		{
			name: "no_upcoming without start parses",
			data: map[string]string{"status": "no_upcoming"},
			assertFn: func(t *testing.T, info *domain.NextStreamInfo) {
				require.NotNil(t, info)
				require.Equal(t, domain.NextStreamStatusNoUpcoming, info.Status)
				require.Nil(t, info.StartScheduled)
			},
		},
		{
			name: "live with trimmed fields",
			data: map[string]string{
				"status":          "live",
				"video_id":        " vid1 ",
				"title":           " Hello ",
				"start_scheduled": " " + scheduledStr + " ",
			},
			assertFn: func(t *testing.T, info *domain.NextStreamInfo) {
				require.NotNil(t, info)
				require.Equal(t, domain.NextStreamStatusLive, info.Status)
				require.Equal(t, "vid1", info.VideoID)
				require.Equal(t, "Hello", info.Title)
				require.NotNil(t, info.StartScheduled)
				require.True(t, info.StartScheduled.Equal(scheduledTime))
			},
		},
		{
			name: "malformed start time returns nil",
			data: map[string]string{
				"status":          "live",
				"start_scheduled": "not-a-time",
			},
			assertFn: func(t *testing.T, info *domain.NextStreamInfo) {
				require.Nil(t, info)
			},
		},
		{
			name: "complete upcoming returns info",
			data: map[string]string{
				"status":          "upcoming",
				"video_id":        "vid9",
				"title":           "Stream",
				"start_scheduled": scheduledStr,
			},
			assertFn: func(t *testing.T, info *domain.NextStreamInfo) {
				require.NotNil(t, info)
				require.Equal(t, domain.NextStreamStatusUpcoming, info.Status)
				require.NotNil(t, info.StartScheduled)
			},
		},
		{
			name: "upcoming missing title returns nil",
			data: map[string]string{
				"status":          "upcoming",
				"video_id":        "vid9",
				"start_scheduled": scheduledStr,
			},
			assertFn: func(t *testing.T, info *domain.NextStreamInfo) {
				require.Nil(t, info)
			},
		},
		{
			name: "upcoming missing start returns nil",
			data: map[string]string{
				"status":   "upcoming",
				"video_id": "vid9",
				"title":    "Stream",
			},
			assertFn: func(t *testing.T, info *domain.NextStreamInfo) {
				require.Nil(t, info)
			},
		},
		{
			name: "upcoming missing video id returns nil",
			data: map[string]string{
				"status":          "upcoming",
				"title":           "Stream",
				"start_scheduled": scheduledStr,
			},
			assertFn: func(t *testing.T, info *domain.NextStreamInfo) {
				require.Nil(t, info)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.assertFn(t, state.ParseNextStreamInfo("UC_alpha", tt.data))
		})
	}
}

func TestBuildTitleFingerprint(t *testing.T) {
	t.Parallel()

	require.Equal(t, BuildTitleFingerprint("title", "vid1"), BuildTitleFingerprint("title", "vid1"))
	require.NotEqual(t, BuildTitleFingerprint("title-a", "vid1"), BuildTitleFingerprint("title-b", "vid1"))
}
