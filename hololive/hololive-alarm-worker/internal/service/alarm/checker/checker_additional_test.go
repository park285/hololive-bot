// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package checker

import (
	"context"
	"errors"
	"testing"
	"time"

	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

func TestCheckerConstructorsValidation(t *testing.T) {
	t.Parallel()

	t.Run("new chzzk checker nil deps", func(t *testing.T) {
		_, err := NewChzzkChecker(nil, &chzzk.Client{}, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache service is nil")

		cacheSvc := newCheckerTestCacheClient(t)

		_, err = NewChzzkChecker(cacheSvc, nil, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chzzk client is nil")
	})

	t.Run("new twitch checker nil deps", func(t *testing.T) {
		_, err := NewTwitchChecker(nil, &twitch.Client{}, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache service is nil")

		cacheSvc := newCheckerTestCacheClient(t)

		_, err = NewTwitchChecker(cacheSvc, nil, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "twitch client is nil")
	})

	t.Run("new youtube checker validation and success", func(t *testing.T) {
		cacheSvc := newCheckerTestCacheClient(t)
		dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, newCheckerTestLogger())
		tierScheduler := tier.NewTieredScheduler(newCheckerTestLogger())

		_, err := NewYouTubeChecker(nil, &holodex.Service{}, tierScheduler, dedupSvc, []int{5}, 0, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache service is nil")

		_, err = NewYouTubeChecker(cacheSvc, nil, tierScheduler, dedupSvc, []int{5}, 0, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "holodex service is nil")

		_, err = NewYouTubeChecker(cacheSvc, &holodex.Service{}, nil, dedupSvc, []int{5}, 0, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tier scheduler is nil")

		_, err = NewYouTubeChecker(cacheSvc, &holodex.Service{}, tierScheduler, nil, []int{5}, 0, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dedup service is nil")

		checker, err := NewYouTubeChecker(
			cacheSvc,
			&holodex.Service{},
			tierScheduler,
			dedupSvc,
			[]int{10, 0, 10},
			0,
			newCheckerTestLogger(),
		)
		require.NoError(t, err)
		require.NotNil(t, checker)
		assert.Equal(t, []int{10, 1}, checker.targetMinutesSnapshot())
		assert.Equal(t, 75*time.Second, checker.evaluationWindowCap)

		checker.UpdateTargetMinutes([]int{15, 0, 15, 3})
		assert.Equal(t, []int{15, 3, 1}, checker.targetMinutesSnapshot())
	})

	t.Run("new notifier nil deps", func(t *testing.T) {
		cacheSvc := newCheckerTestCacheClient(t)
		dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, newCheckerTestLogger())
		queuePublisher := queue.NewPublisher(cacheSvc, newCheckerTestLogger())

		_, err := NewNotifier(nil, queuePublisher, nil, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dedup service is nil")

		_, err = NewNotifier(dedupSvc, nil, nil, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "queue publisher is nil")
	})
}

func TestCommonHelperFunctions(t *testing.T) {
	t.Parallel()

	t.Run("unique strings", func(t *testing.T) {
		input := []string{"a", "", "b", "a", "c", "b"}
		assert.Equal(t, []string{"a", "b", "c"}, uniqueStrings(input))
		assert.Equal(t, []string{"x"}, uniqueStrings([]string{"x"}))
	})

	t.Run("clone stream deep copy", func(t *testing.T) {
		now := time.Date(2026, time.March, 5, 1, 0, 0, 0, time.UTC)
		channel := &domain.Channel{ID: "ch1", Name: "name"}
		stream := &domain.Stream{ID: "s1", StartScheduled: &now, StartActual: &now, Channel: channel}

		cloned := cloneStream(stream)
		require.NotNil(t, cloned)
		require.NotSame(t, stream, cloned)
		require.NotSame(t, stream.Channel, cloned.Channel)
		require.NotSame(t, stream.StartScheduled, cloned.StartScheduled)

		cloned.Channel.Name = "changed"
		assert.Equal(t, "name", stream.Channel.Name)
	})

	t.Run("ensure scheduled time", func(t *testing.T) {
		fallback := time.Date(2026, time.March, 5, 1, 3, 45, 0, time.FixedZone("KST", 9*60*60))
		actual := time.Date(2026, time.March, 5, 1, 1, 30, 0, time.FixedZone("KST", 9*60*60))

		streamWithSchedule := &domain.Stream{StartScheduled: &fallback}
		assert.Same(t, streamWithSchedule, ensureScheduledTime(streamWithSchedule, fallback))

		streamWithActual := &domain.Stream{StartActual: &actual}
		updated := ensureScheduledTime(streamWithActual, fallback)
		require.NotNil(t, updated)
		require.NotNil(t, updated.StartScheduled)
		assert.Equal(t, actual.UTC(), *updated.StartScheduled)

		streamWithoutTimes := &domain.Stream{}

		updated = ensureScheduledTime(streamWithoutTimes, fallback)
		require.NotNil(t, updated.StartScheduled)
		assert.Equal(t, fallback.UTC().Truncate(time.Minute), *updated.StartScheduled)

		assert.Nil(t, ensureScheduledTime(nil, fallback))
	})

	t.Run("room notifications", func(t *testing.T) {
		stream := &domain.Stream{ID: "s1"}
		channel := &domain.Channel{ID: "ch1"}

		notifications := roomNotifications([]string{"room1", "", "room2"}, channel, stream, 5, "msg")
		require.Len(t, notifications, 2)
		assert.Equal(t, "room1", notifications[0].RoomID)
		assert.Equal(t, 5, notifications[0].MinutesUntil)

		assert.Nil(t, roomNotifications(nil, channel, stream, 0, ""))
		assert.Nil(t, roomNotifications([]string{"room1"}, channel, nil, 0, ""))
	})

	t.Run("normalize target minutes", func(t *testing.T) {
		assert.Equal(t, []int{5, 3, 1}, sharedchecker.NormalizeTargetMinutes(nil))
		assert.Equal(t, []int{5, 3, 1}, sharedchecker.NormalizeTargetMinutes([]int{0, -1}))
		assert.Equal(t, []int{10, 5, 1}, sharedchecker.NormalizeTargetMinutes([]int{5, 10, 10, 0}))
		assert.Equal(t, []int{10, 3, 1}, sharedchecker.NormalizeTargetMinutes([]int{1, 3, 10, 3}))
	})

	t.Run("safe logger", func(t *testing.T) {
		require.NotNil(t, safeLogger(nil))

		logger := newCheckerTestLogger()
		assert.Same(t, logger, safeLogger(logger))
	})

	t.Run("youtube upcoming selection label", func(t *testing.T) {
		assert.Equal(t, "schedule_change_only", youtubeUpcomingSelectionLabel(4, 4, false))
		assert.Equal(t, "recovered_crossing", youtubeUpcomingSelectionLabel(5, 3, true))
		assert.Equal(t, "current_bucket", youtubeUpcomingSelectionLabel(3, 3, true))
		assert.Equal(t, "lower_than_current", youtubeUpcomingSelectionLabel(1, 3, true))
	})
}

func TestLoadSubscriberRoomsByChannel(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		cacheSvc := newCheckerTestCacheClient(t)
		ctx := t.Context()

		_, err := cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"ch1", []string{"room1", "room2"})
		require.NoError(t, err)

		result, err := loadSubscriberRoomsByChannel(ctx, cacheSvc, []string{"ch1", "ch1", "ch2"})
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.ElementsMatch(t, []string{"room1", "room2"}, result["ch1"])
	})

	t.Run("empty input", func(t *testing.T) {
		result, err := loadSubscriberRoomsByChannel(t.Context(), cachemocks.NewStrictClient(), nil)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("cache error", func(t *testing.T) {
		mockCache := &cachemocks.Client{
			SMembersFunc: func(context.Context, string) ([]string, error) {
				return nil, errors.New("smembers failed")
			},
		}
		_, err := loadSubscriberRoomsByChannel(t.Context(), mockCache, []string{"ch1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "smembers channel ch1")
	})

	t.Run("uses pipelined smembers lookup", func(t *testing.T) {
		baseCache := newCheckerTestCacheClient(t)
		countingCache := &countingCheckerCacheClient{Client: baseCache}
		ctx := t.Context()

		_, err := countingCache.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"ch1", []string{"room1", "room2"})
		require.NoError(t, err)

		_, err = countingCache.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"ch2", []string{"room3"})
		require.NoError(t, err)

		result, err := loadSubscriberRoomsByChannel(ctx, countingCache, []string{"ch1", "ch2", "ch1"})
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.ElementsMatch(t, []string{"room1", "room2"}, result["ch1"])
		assert.ElementsMatch(t, []string{"room3"}, result["ch2"])
		assert.Equal(t, 1, countingCache.doMultiCalls)
		assert.Zero(t, countingCache.sMembersCalls)
	})
}

type countingCheckerCacheClient struct {
	cache.Client
	doMultiCalls  int
	sMembersCalls int
}

func (c *countingCheckerCacheClient) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	c.doMultiCalls++
	return c.Client.DoMulti(ctx, cmds...)
}

func (c *countingCheckerCacheClient) SMembers(ctx context.Context, key string) ([]string, error) {
	c.sMembersCalls++
	return c.Client.SMembers(ctx, key)
}

func TestChzzkHelperFunctions(t *testing.T) {
	t.Parallel()

	assert.False(t, isChzzkLive(nil))
	assert.True(t, isChzzkLive(&chzzk.LiveStatusContent{Status: "OPEN"}))
	assert.True(t, isChzzkLive(&chzzk.LiveStatusContent{Status: "open"}))
	assert.False(t, isChzzkLive(&chzzk.LiveStatusContent{Status: "CLOSE"}))

	detected := time.Date(2026, time.March, 5, 2, 4, 59, 0, time.UTC)
	assert.Equal(
		t,
		notification.ChzzkLiveNotifiedKeyPrefix+"chzzk1:20260305T0200",
		buildChzzkLiveDedupKey("chzzk1", detected),
	)

	stream := buildChzzkLiveStream("yt1", "chzzk1", nil, detected)
	require.NotNil(t, stream)
	assert.Equal(t, domain.StreamStatusLive, stream.Status)
	assert.Equal(t, "치지직 라이브", stream.Title)
	assert.Equal(t, "yt1", stream.ChannelID)
	assert.Equal(t, "yt1", stream.ChannelName)
	assert.True(t, stream.IsChzzkOnly)
	assert.Equal(t, "https://chzzk.naver.com/live/chzzk1", stream.ChzzkLiveURL)

	status := &chzzk.LiveStatusContent{
		Status:              "OPEN",
		LiveTitle:           "  치지직 타이틀  ",
		LiveCategoryValue:   "게임",
		ConcurrentUserCount: 777,
	}

	stream = buildChzzkLiveStream("yt2", "chzzk2", status, detected)
	require.NotNil(t, stream.ViewerCount)
	assert.Equal(t, 777, *stream.ViewerCount)
	assert.Equal(t, "치지직 타이틀", stream.Title)
	assert.Equal(t, "yt2", stream.ChannelName)
}

func TestTwitchHelperFunctions(t *testing.T) {
	t.Parallel()

	mappings, channelIDs := normalizeTwitchLoginMappings(map[string]string{
		" Aqua ": " ch1 ",
		"":       "ch2",
		"SuI":    "",
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

	assert.Nil(t, buildTwitchLiveStream("yt", nil))

	startedAt := time.Date(2026, time.March, 5, 3, 0, 0, 0, time.UTC)
	stream := buildTwitchLiveStream("yt1", &twitch.StreamData{
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
	assert.Equal(t, "AquaName", stream.ChannelName)
	assert.Equal(t, "Twitch Live", stream.Title)
	assert.Equal(t, "twitch:user-1:stream-1", stream.ID)
	require.NotNil(t, stream.ViewerCount)
	assert.Equal(t, 321, *stream.ViewerCount)
	assert.Equal(t, "user-1", stream.TwitchUserID)
	assert.Equal(t, "aqua", stream.TwitchUserLogin)
	assert.Equal(t, "stream-1", stream.TwitchStreamID)
	assert.Equal(t, "https://twitch.tv/aqua", stream.TwitchLiveURL)
	assert.True(t, stream.IsTwitchOnly)

	fallback := buildTwitchLiveStream("yt2", &twitch.StreamData{
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
				Type:      "", // not live
				StartedAt: startedAt,
			},
		},
	}

	t.Run("success without checker-level dedup preclaim", func(t *testing.T) {
		setNXCalls := 0
		checker := &TwitchChecker{
			cacheSvc: &cachemocks.Client{
				SetNXFunc: func(context.Context, string, string, time.Duration) (bool, error) {
					setNXCalls++
					return false, errors.New("checker must not preclaim dedup")
				},
			},
			logger: newCheckerTestLogger(),
		}

		notifications, err := checker.buildLiveNotifications(
			t.Context(),
			map[string]string{"aqua": "ch1"},
			map[string][]string{"ch1": {"room1", "room2"}},
			liveResponse,
		)
		require.NoError(t, err)
		require.Len(t, notifications, 2)
		assert.ElementsMatch(t, []string{"room1", "room2"}, []string{notifications[0].RoomID, notifications[1].RoomID})

		notifications, err = checker.buildLiveNotifications(
			t.Context(),
			map[string]string{"aqua": "ch1"},
			map[string][]string{"ch1": {"room1", "room2"}},
			liveResponse,
		)
		require.NoError(t, err)
		require.Len(t, notifications, 2)
		assert.Equal(t, 0, setNXCalls, "checker must not preclaim dedup before queue publish")
	})
}

func TestYouTubeHelperFunctions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 5, 5, 0, 0, 0, time.UTC)

	assert.Nil(t, resolveLiveStart(nil))

	scheduled := now.Add(10 * time.Minute)
	start := resolveLiveStart(&domain.Stream{StartScheduled: &scheduled})
	require.NotNil(t, start)
	assert.Equal(t, scheduled.UTC(), *start)

	actual := now.Add(-2 * time.Minute)

	start = resolveLiveStart(&domain.Stream{StartScheduled: &scheduled, StartActual: &actual})
	require.NotNil(t, start)
	assert.Equal(t, actual.UTC(), *start)

	grouped := groupStreamsByChannel([]*domain.Stream{
		{ID: "s1", ChannelID: "ch1"},
		{ID: "s2", Channel: &domain.Channel{ID: "ch2"}},
		{ID: "s3"},
		nil,
	})
	require.Len(t, grouped, 2)
	assert.Len(t, grouped["ch1"], 1)
	assert.Len(t, grouped["ch2"], 1)
}

func TestYouTubeNotificationBuilders(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, newCheckerTestLogger())
	checker := &YouTubeChecker{
		dedupSvc:            dedupSvc,
		targetPolicy:        sharedchecker.NewTargetMinutePolicy([]int{5, 3, 1}),
		evaluationWindowCap: 75 * time.Second,
		logger:              newCheckerTestLogger(),
	}

	ctx := t.Context()
	now := time.Date(2026, time.March, 5, 6, 0, 0, 0, time.UTC)

	t.Run("build upcoming notifications", func(t *testing.T) {
		start := now.Add(5 * time.Minute)
		stream := &domain.Stream{
			ID:             "upcoming-1",
			ChannelID:      "ch1",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &start,
			Channel:        &domain.Channel{ID: "ch1", Name: "Channel 1"},
		}

		window := sharedchecker.EvaluationWindow{
			Start: now.Add(-75 * time.Second),
			End:   now,
		}

		notifications, err := checker.buildUpcomingNotifications(ctx, stream, []string{"room1", "room2"}, window)
		require.NoError(t, err)
		require.Len(t, notifications, 2)
		assert.Equal(t, 5, notifications[0].MinutesUntil)

		require.NoError(t, dedupSvc.MarkAsNotified(ctx, stream.ID, start, 5))

		notifications, err = checker.buildUpcomingNotifications(ctx, stream, []string{"room1"}, window)
		require.NoError(t, err)
		assert.Empty(t, notifications)

		nonTarget := now.Add(10 * time.Minute)

		notifications, err = checker.buildUpcomingNotifications(ctx, &domain.Stream{
			ID:             "upcoming-2",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &nonTarget,
		}, []string{"room1"}, window)
		require.NoError(t, err)
		assert.Empty(t, notifications)
	})

	t.Run("build upcoming notifications across crossed target window", func(t *testing.T) {
		start := now.Add(4*time.Minute + 20*time.Second)
		stream := &domain.Stream{
			ID:             "upcoming-crossed",
			ChannelID:      "ch1",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &start,
			Channel:        &domain.Channel{ID: "ch1", Name: "Channel 1"},
		}

		window := sharedchecker.EvaluationWindow{
			Start: now.Add(-40 * time.Second),
			End:   now,
		}

		notifications, err := checker.buildUpcomingNotifications(ctx, stream, []string{"room1"}, window)
		require.NoError(t, err)
		require.Len(t, notifications, 1)
		assert.Equal(t, 5, notifications[0].MinutesUntil)
	})

	t.Run("build upcoming notifications backfills five minute target on initial capped observation", func(t *testing.T) {
		start := now.Add(4*time.Minute + 20*time.Second)
		stream := &domain.Stream{
			ID:             "upcoming-initial-five",
			ChannelID:      "ch1",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &start,
			Channel:        &domain.Channel{ID: "ch1", Name: "Channel 1"},
		}

		window := sharedchecker.EvaluationWindow{
			Start:              now.Add(-75 * time.Second),
			End:                now,
			Capped:             true,
			InitialObservation: true,
		}

		notifications, err := checker.buildUpcomingNotifications(ctx, stream, []string{"room1"}, window)
		require.NoError(t, err)
		require.Len(t, notifications, 1)
		assert.Equal(t, 5, notifications[0].MinutesUntil)
	})

	t.Run("build upcoming notifications recovers recent capped five minute target after initial observation", func(t *testing.T) {
		start := now.Add(4 * time.Minute)
		stream := &domain.Stream{
			ID:             "upcoming-stale-five",
			ChannelID:      "ch1",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &start,
			Channel:        &domain.Channel{ID: "ch1", Name: "Channel 1"},
		}

		window := sharedchecker.EvaluationWindow{
			Start:              now.Add(-75 * time.Second),
			End:                now,
			Capped:             true,
			InitialObservation: false,
		}

		notifications, err := checker.buildUpcomingNotifications(ctx, stream, []string{"room1"}, window)
		require.NoError(t, err)
		require.Len(t, notifications, 1)
		assert.Equal(t, 5, notifications[0].MinutesUntil)
	})

	t.Run("build live catchup notifications as missed primary reminder", func(t *testing.T) {
		start := now.Add(-3 * time.Minute)
		stream := &domain.Stream{
			ID:             "live-1",
			Title:          "live title",
			ChannelID:      "ch-live",
			Status:         domain.StreamStatusLive,
			StartScheduled: &start,
			StartActual:    &start,
			Channel:        &domain.Channel{ID: "ch-live", Name: "Live Channel"},
		}

		notifications, err := checker.buildLiveCatchupNotifications(ctx, "ch-live", stream, []string{"room1", "room2"}, now)
		require.NoError(t, err)
		require.Len(t, notifications, 2)
		assert.Equal(t, 5, notifications[0].MinutesUntil)

		require.NoError(t, dedupSvc.MarkUpcomingEventNotified(ctx, "room1", "ch-live", stream))

		notifications, err = checker.buildLiveCatchupNotifications(ctx, "ch-live", stream, []string{"room1", "room2"}, now)
		require.NoError(t, err)
		require.Len(t, notifications, 1)
		assert.Equal(t, "room2", notifications[0].RoomID)
		assert.Equal(t, 5, notifications[0].MinutesUntil)

		require.NoError(t, dedupSvc.MarkAsNotified(ctx, stream.ID, start, 5))

		notifications, err = checker.buildLiveCatchupNotifications(ctx, "ch-live", stream, []string{"room1", "room2"}, now)
		require.NoError(t, err)
		require.Len(t, notifications, 1)
		assert.Equal(t, "room2", notifications[0].RoomID)
		assert.Equal(t, 5, notifications[0].MinutesUntil)

		oldStart := now.Add(-10 * time.Minute)
		oldStream := &domain.Stream{ID: "live-old", Status: domain.StreamStatusLive, StartScheduled: &oldStart}

		notifications, err = checker.buildLiveCatchupNotifications(ctx, "ch-live", oldStream, []string{"room1"}, now)
		require.NoError(t, err)
		assert.Empty(t, notifications)

		futureStart := now.Add(2 * time.Minute)
		futureStream := &domain.Stream{ID: "live-future", Status: domain.StreamStatusLive, StartScheduled: &futureStart}

		notifications, err = checker.buildLiveCatchupNotifications(ctx, "ch-live", futureStream, []string{"room1"}, now)
		require.NoError(t, err)
		assert.Empty(t, notifications)
	})

	t.Run("build channel notifications", func(t *testing.T) {
		upcomingStart := now.Add(5 * time.Minute)
		liveStart := now.Add(-2 * time.Minute)
		streams := []*domain.Stream{
			{
				ID:             "channel-upcoming",
				ChannelID:      "ch-1",
				Status:         domain.StreamStatusUpcoming,
				StartScheduled: &upcomingStart,
				Channel:        &domain.Channel{ID: "ch-1"},
			},
			{
				ID:             "channel-live",
				ChannelID:      "ch-1",
				Status:         domain.StreamStatusLive,
				StartScheduled: &liveStart,
				StartActual:    &liveStart,
				Channel:        &domain.Channel{ID: "ch-1"},
			},
		}

		window := sharedchecker.EvaluationWindow{
			Start: now.Add(-45 * time.Second),
			End:   now,
		}

		notifications, err := checker.buildChannelNotifications(ctx, "ch-1", []string{"room1", "room2"}, streams, window, now)
		require.NoError(t, err)
		assert.NotEmpty(t, notifications)
	})
}

func TestResolveEligibleLiveCatchupStartUsesLiveCatchupWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	inWindow := now.Add(-sharedconstants.LiveCatchupWindow)
	stream := &domain.Stream{
		ID:             "live-in-window",
		Status:         domain.StreamStatusLive,
		StartScheduled: &inWindow,
	}

	got, ok := resolveEligibleLiveCatchupStart(stream, now)
	require.True(t, ok)
	require.NotNil(t, got)

	outside := now.Add(-(sharedconstants.LiveCatchupWindow + time.Second))
	stream.StartScheduled = &outside

	got, ok = resolveEligibleLiveCatchupStart(stream, now)
	require.False(t, ok)
	require.Nil(t, got)
}

func TestLiveCatchupSuppressesRoomsAfterPublishedMarker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	start := now.Add(-2 * time.Minute)

	checker, dedupSvc := newTestYouTubeCheckerWithDedup(t)

	stream := &domain.Stream{
		ID:             "live-dedup",
		Title:          "live title",
		ChannelID:      "ch-live",
		Status:         domain.StreamStatusLive,
		StartScheduled: &start,
		StartActual:    &start,
		Channel:        &domain.Channel{ID: "ch-live", Name: "Live Channel"},
	}

	first, err := checker.buildLiveCatchupNotifications(ctx, "ch-live", stream, []string{"room1", "room2"}, now)
	require.NoError(t, err)
	require.Len(t, first, 2)
	assert.Equal(t, 5, first[0].MinutesUntil)

	require.NoError(t, dedupSvc.MarkAsNotified(ctx, stream.ID, start, 5))
	require.NoError(t, dedupSvc.MarkUpcomingEventNotified(ctx, "room1", "ch-live", stream))

	second, err := checker.buildLiveCatchupNotifications(ctx, "ch-live", stream, []string{"room1", "room2"}, now)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, "room2", second[0].RoomID)
	assert.Equal(t, 5, second[0].MinutesUntil)
}

func TestLiveCatchupAllowsRescheduledStreamAfterPreviousScheduleNotified(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	oldStart := now.Add(-30 * time.Minute)
	newStart := now.Add(-2 * time.Minute)

	checker, dedupSvc := newTestYouTubeCheckerWithDedup(t)
	require.NoError(t, dedupSvc.MarkAsNotified(ctx, "live-rescheduled", oldStart, 5))

	stream := &domain.Stream{
		ID:             "live-rescheduled",
		Title:          "rescheduled title",
		ChannelID:      "ch-live",
		Status:         domain.StreamStatusLive,
		StartScheduled: &newStart,
		StartActual:    &newStart,
		Channel:        &domain.Channel{ID: "ch-live", Name: "Live Channel"},
	}

	notifications, err := checker.buildLiveCatchupNotifications(ctx, "ch-live", stream, []string{"room1"}, now)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, 5, notifications[0].MinutesUntil)
}

func newTestYouTubeCheckerWithDedup(t *testing.T) (*YouTubeChecker, *dedup.Service) {
	t.Helper()

	cacheSvc := newCheckerTestCacheClient(t)
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, newCheckerTestLogger())
	checker := &YouTubeChecker{
		dedupSvc:            dedupSvc,
		targetPolicy:        sharedchecker.NewTargetMinutePolicy([]int{5, 3, 1}),
		evaluationWindowCap: 75 * time.Second,
		logger:              newCheckerTestLogger(),
	}
	return checker, dedupSvc
}

func TestNotifierReleaseClaimsBestEffort(t *testing.T) {
	t.Parallel()

	notifier := &Notifier{
		dedupSvc: dedup.NewService(&cachemocks.Client{
			DelManyFunc: func(context.Context, []string) (int64, error) {
				return 0, errors.New("delmany failed")
			},
		}, []int{5, 3, 1}, newCheckerTestLogger()),
		logger: newCheckerTestLogger(),
	}

	// error should be swallowed as best-effort path.
	notifier.releaseClaimsBestEffort(t.Context(), []string{"claim-1"}, "release failed")
	notifier.releaseClaimsBestEffort(t.Context(), nil, "release failed")
}

var _ cache.Client = (*cachemocks.Client)(nil)
