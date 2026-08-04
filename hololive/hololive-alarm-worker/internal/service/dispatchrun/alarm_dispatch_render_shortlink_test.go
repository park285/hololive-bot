package dispatchrun

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAlarmDispatchGroupViewUsesShortLinksForYouTube(t *testing.T) {
	t.Parallel()

	builder, err := shortlinkservice.NewYouTubeBuilder("https://go.example.com")
	require.NoError(t, err)
	group := alarmDispatchGroup{
		minutesUntil: 5,
		notifications: []domain.AlarmNotification{
			alarmShortLinkNotification("dQw4w9WgXcQ", "First"),
			alarmShortLinkNotification("abcdefghijk", "Second"),
		},
	}

	view := buildAlarmDispatchGroupViewWithShortLinks(t.Context(), nil, group, builder)

	require.Len(t, view.Entries, 2)
	assert.Equal(t, "https://go.example.com/l/dQw4w9WgXcQ", view.Entries[0].URL)
	assert.Equal(t, "https://go.example.com/l/abcdefghijk", view.Entries[1].URL)
}

func TestBuildAlarmDispatchGroupViewPreservesDirectPlatformLinks(t *testing.T) {
	t.Parallel()

	builder, err := shortlinkservice.NewYouTubeBuilder("https://go.example.com")
	require.NoError(t, err)
	twitch := alarmShortLinkNotification("abcdefghijk", "Twitch")
	twitch.Stream.IsTwitchOnly = true
	twitch.Stream.TwitchLiveURL = "https://twitch.tv/member"

	view := buildAlarmDispatchGroupViewWithShortLinks(t.Context(), nil, alarmDispatchGroup{
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{twitch},
	}, builder)

	require.Len(t, view.Entries, 1)
	assert.Equal(t, "https://twitch.tv/member", view.Entries[0].URL)
}

func TestBuildAlarmDispatchGroupViewFallsBackForInvalidVideoID(t *testing.T) {
	t.Parallel()

	builder, err := shortlinkservice.NewYouTubeBuilder("https://go.example.com")
	require.NoError(t, err)
	notification := alarmShortLinkNotification("invalid", "Fallback")

	view := buildAlarmDispatchGroupViewWithShortLinks(t.Context(), nil, alarmDispatchGroup{
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{notification},
	}, builder)

	require.Len(t, view.Entries, 1)
	assert.Equal(t, domain.YouTubeWatchURL("invalid"), view.Entries[0].URL)
}

func TestBuildAlarmDispatchGroupViewPreservesIntegratedSecondaryLink(t *testing.T) {
	t.Parallel()

	builder, err := shortlinkservice.NewYouTubeBuilder("https://go.example.com")
	require.NoError(t, err)
	integrated := alarmShortLinkNotification("dQw4w9WgXcQ", "Integrated")
	integrated.Stream.IsIntegrated = true
	integrated.Stream.ChzzkLiveURL = "https://chzzk.naver.com/live/channel"

	view := buildAlarmDispatchGroupViewWithShortLinks(t.Context(), nil, alarmDispatchGroup{
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{integrated},
	}, builder)

	require.Len(t, view.Entries, 1)
	assert.Equal(
		t,
		"https://go.example.com/l/dQw4w9WgXcQ | https://chzzk.naver.com/live/channel",
		view.Entries[0].URL,
	)
}

func TestBuildAlarmDispatchItemViewKeepsSingleNotificationURL(t *testing.T) {
	t.Parallel()

	notification := alarmShortLinkNotification("dQw4w9WgXcQ", "Single")

	view := buildAlarmDispatchItemView(t.Context(), nil, &notification, -1)

	assert.Equal(t, domain.YouTubeWatchURL("dQw4w9WgXcQ"), view.URL)
}

func TestRenderAlarmDispatchNotificationGroupUsesConfiguredShortLinks(t *testing.T) {
	t.Setenv(alarmShortLinkBaseURLEnv, "https://go.example.com")
	renderer, store := newAlarmDispatchTestRendering(t)
	group := alarmDispatchGroup{
		minutesUntil: 5,
		notifications: []domain.AlarmNotification{
			alarmShortLinkNotification("dQw4w9WgXcQ", "First"),
			alarmShortLinkNotification("abcdefghijk", "Second"),
		},
	}

	message, err := renderAlarmDispatchNotificationGroup(t.Context(), renderer, store, group)

	require.NoError(t, err)
	assert.Contains(t, message, "https://go.example.com/l/dQw4w9WgXcQ")
	assert.Contains(t, message, "https://go.example.com/l/abcdefghijk")
	assert.NotContains(t, message, domain.YouTubeWatchURL("dQw4w9WgXcQ"))
}

func TestRenderAlarmDispatchNotificationGroupRejectsInvalidShortLinkConfig(t *testing.T) {
	t.Setenv(alarmShortLinkBaseURLEnv, "http://go.example.com")
	renderer, store := newAlarmDispatchTestRendering(t)
	group := alarmDispatchGroup{
		minutesUntil: 5,
		notifications: []domain.AlarmNotification{
			alarmShortLinkNotification("dQw4w9WgXcQ", "First"),
			alarmShortLinkNotification("abcdefghijk", "Second"),
		},
	}

	message, err := renderAlarmDispatchNotificationGroup(t.Context(), renderer, store, group)

	require.Error(t, err)
	assert.Empty(t, message)
	assert.Contains(t, err.Error(), alarmShortLinkBaseURLEnv)
}

func alarmShortLinkNotification(videoID, title string) domain.AlarmNotification {
	return domain.AlarmNotification{
		Channel:      &domain.Channel{Name: "Member"},
		Stream:       &domain.Stream{ID: videoID, Title: title},
		MinutesUntil: 5,
	}
}
