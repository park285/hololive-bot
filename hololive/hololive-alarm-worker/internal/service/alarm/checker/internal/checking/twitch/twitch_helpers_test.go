package twitch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

func TestNewTwitchCheckerValidation(t *testing.T) {
	t.Parallel()

	_, err := NewTwitchChecker(nil, &twitch.Client{}, newCheckerTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache service is nil")

	cache := newCheckerTestCacheClient(t)

	_, err = NewTwitchChecker(cache, nil, newCheckerTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "twitch client is nil")
}

func TestTwitchCheckerCheck_EmptyMappings(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	checker, err := NewTwitchChecker(
		cache,
		twitch.NewClient(&twitch.ClientConfig{}, newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	require.NoError(t, err)

	notifications, checkErr := checker.Check(t.Context())
	require.NoError(t, checkErr)
	assert.Empty(t, notifications)
}

func TestTwitchHelperFunctions(t *testing.T) {
	t.Parallel()

	spacedLogin := " " + "Aqua" + " "
	spacedChannelID := " " + "ch1" + " "
	mappings, channelIDs := normalizeTwitchLoginMappings(map[string]string{
		spacedLogin: spacedChannelID,
		"":          "ch2",
		"SuI":       "",
	})
	require.Len(t, mappings, 1)
	assert.Equal(t, "ch1", mappings["aqua"])
	assert.Equal(t, []string{"ch1"}, channelIDs)

	lookup := buildTwitchLookupLogins(
		map[string]string{"aqua": "ch1", "sui": "ch2"},
		map[string][]string{"ch1": {"room1"}, "ch2": {}},
	)
	assert.Equal(t, []string{"aqua"}, lookup)

	assert.Equal(t, twitchLiveNotifiedKeyPrefix+"u1:s1", buildTwitchLiveDedupKey("u1", "s1"))

	assert.Nil(t, buildTwitchLiveStream("yt", "", nil))

	startedAt := time.Date(2026, time.March, 5, 3, 0, 0, 0, time.UTC)
	stream := buildTwitchLiveStream("yt1", "아쿠아", &twitch.StreamData{
		ID:          "stream-1",
		UserID:      "user-1",
		UserLogin:   " Aqua ",
		UserName:    "AquaName",
		Title:       "  Twitch Live  ",
		ViewerCount: 321,
		StartedAt:   startedAt,
		Type:        "live",
	})
	require.NotNil(t, stream)
	assert.Equal(t, domain.StreamStatusLive, stream.Status)
	assert.Equal(t, "yt1", stream.ChannelID)
	assert.Equal(t, "아쿠아", stream.ChannelName)
	assert.Equal(t, "Twitch Live", stream.Title)
	assert.Equal(t, "twitch:user-1:stream-1", stream.ID)
	require.NotNil(t, stream.ViewerCount)
	assert.Equal(t, 321, *stream.ViewerCount)
	assert.Equal(t, "user-1", stream.TwitchUserID)
	assert.Equal(t, "aqua", stream.TwitchUserLogin)
	assert.Equal(t, "stream-1", stream.TwitchStreamID)
	assert.Equal(t, "https://twitch.tv/aqua", stream.TwitchLiveURL)
	assert.True(t, stream.IsTwitchOnly)

	fallback := buildTwitchLiveStream("yt2", "", &twitch.StreamData{
		ID:        "stream-2",
		UserLogin: "NoID",
		StartedAt: startedAt,
		Type:      "live",
	})
	require.NotNil(t, fallback)
	assert.Equal(t, "twitch:noid:stream-2", fallback.ID)
	assert.Equal(t, "noid", fallback.TwitchUserID)
	assert.Equal(t, "NoID", fallback.ChannelName)
	assert.Equal(t, "Twitch 라이브", fallback.Title)
}

func TestTwitchBuildLiveNotifications(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.March, 5, 4, 0, 0, 0, time.UTC)
	liveResponse := &twitch.StreamsResponse{
		Data: []twitch.StreamData{
			{
				ID:        "stream-1",
				UserID:    "user-1",
				UserLogin: "AQUA",
				UserName:  "Aqua",
				Title:     "live now",
				Type:      "live",
				StartedAt: startedAt,
			},
			{
				ID:        "stream-2",
				UserID:    "user-2",
				UserLogin: "sui",
				Type:      "",
				StartedAt: startedAt,
			},
		},
	}

	t.Run("success without checker-level dedup preclaim", func(t *testing.T) {
		setNXCalls := 0
		checker := &TwitchChecker{
			cacheClient: &cachemocks.Client{
				SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
					setNXCalls++
					return false, errors.New("checker must not preclaim dedup")
				},
			},
			logger: newCheckerTestLogger(),
		}

		notifications := checker.buildLiveNotifications(
			map[string]string{"aqua": "ch1"},
			map[string][]string{"ch1": {"room1", "room2"}},
			map[string]string{"ch1": "아쿠아"},
			liveResponse,
		)
		require.Len(t, notifications, 2)
		assert.ElementsMatch(t, []string{"room1", "room2"}, []string{notifications[0].RoomID, notifications[1].RoomID})

		notifications = checker.buildLiveNotifications(
			map[string]string{"aqua": "ch1"},
			map[string][]string{"ch1": {"room1", "room2"}},
			map[string]string{"ch1": "아쿠아"},
			liveResponse,
		)
		require.Len(t, notifications, 2)
		assert.Equal(t, 0, setNXCalls, "checker must not preclaim dedup before queue publish")
	})
}
