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

package checking

import (
	"context"
	"errors"
	"testing"
	"time"

	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/notification"
)

func TestCheckerConstructorsValidation(t *testing.T) {
	t.Parallel()

	t.Run("new youtube checker validation and success", func(t *testing.T) {
		cache := newCheckerTestCacheClient(t)
		dedupService := dedup.NewService(cache, []int{5, 3, 1}, newCheckerTestLogger())
		tierScheduler := tier.NewTieredScheduler(newCheckerTestLogger())

		_, err := NewYouTubeChecker(nil, &holodex.Service{}, tierScheduler, dedupService, []int{5}, 0, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache service is nil")

		_, err = NewYouTubeChecker(cache, nil, tierScheduler, dedupService, []int{5}, 0, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "holodex service is nil")

		_, err = NewYouTubeChecker(cache, &holodex.Service{}, nil, dedupService, []int{5}, 0, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tier scheduler is nil")

		_, err = NewYouTubeChecker(cache, &holodex.Service{}, tierScheduler, nil, []int{5}, 0, newCheckerTestLogger())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dedup service is nil")

		checker, err := NewYouTubeChecker(
			cache,
			&holodex.Service{},
			tierScheduler,
			dedupService,
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
}

func TestCommonHelperFunctions(t *testing.T) {
	t.Parallel()

	t.Run("unique strings", func(t *testing.T) {
		input := []string{"a", "", "b", "a", "c", "b"}
		assert.Equal(t, []string{"a", "b", "c"}, UniqueStrings(input))
		assert.Equal(t, []string{"x"}, UniqueStrings([]string{"x"}))
	})

	t.Run("clone stream deep copy", func(t *testing.T) {
		now := time.Date(2026, time.March, 5, 1, 0, 0, 0, time.UTC)
		channel := &domain.Channel{ID: "ch1", Name: "name"}
		stream := &domain.Stream{ID: "s1", StartScheduled: &now, StartActual: &now, Channel: channel}

		cloned := CloneStream(stream)
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
		assert.Same(t, streamWithSchedule, EnsureScheduledTime(streamWithSchedule, fallback))

		streamWithActual := &domain.Stream{StartActual: &actual}
		updated := EnsureScheduledTime(streamWithActual, fallback)
		require.NotNil(t, updated)
		require.NotNil(t, updated.StartScheduled)
		assert.Equal(t, actual.UTC(), *updated.StartScheduled)

		streamWithoutTimes := &domain.Stream{}

		updated = EnsureScheduledTime(streamWithoutTimes, fallback)
		require.NotNil(t, updated.StartScheduled)
		assert.Equal(t, fallback.UTC().Truncate(time.Minute), *updated.StartScheduled)

		assert.Nil(t, EnsureScheduledTime(nil, fallback))
	})

	t.Run("room notifications", func(t *testing.T) {
		stream := &domain.Stream{ID: "s1"}
		channel := &domain.Channel{ID: "ch1"}

		notifications := RoomNotifications([]string{"room1", "", "room2"}, channel, stream, 5, "msg")
		require.Len(t, notifications, 2)
		assert.Equal(t, "room1", notifications[0].RoomID)
		assert.Equal(t, 5, notifications[0].MinutesUntil)

		assert.Nil(t, RoomNotifications(nil, channel, stream, 0, ""))
		assert.Nil(t, RoomNotifications([]string{"room1"}, channel, nil, 0, ""))
	})

	t.Run("normalize target minutes", func(t *testing.T) {
		assert.Equal(t, []int{5, 3, 1}, sharedchecker.NormalizeTargetMinutes(nil))
		assert.Equal(t, []int{5, 3, 1}, sharedchecker.NormalizeTargetMinutes([]int{0, -1}))
		assert.Equal(t, []int{10, 5, 1}, sharedchecker.NormalizeTargetMinutes([]int{5, 10, 10, 0}))
		assert.Equal(t, []int{10, 3, 1}, sharedchecker.NormalizeTargetMinutes([]int{1, 3, 10, 3}))
	})

	t.Run("safe logger", func(t *testing.T) {
		require.NotNil(t, SafeLogger(nil))

		logger := newCheckerTestLogger()
		assert.Same(t, logger, SafeLogger(logger))
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
		cache := newCheckerTestCacheClient(t)
		ctx := t.Context()

		_, err := cache.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"ch1", []string{"room1", "room2"})
		require.NoError(t, err)

		result, err := LoadSubscriberRoomsByChannel(ctx, cache, []string{"ch1", "ch1", "ch2"})
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.ElementsMatch(t, []string{"room1", "room2"}, result["ch1"])
	})

	t.Run("empty input", func(t *testing.T) {
		result, err := LoadSubscriberRoomsByChannel(t.Context(), cachemocks.NewStrictClient(), nil)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("cache error", func(t *testing.T) {
		mockCache := &cachemocks.Client{
			SMembersFunc: func(context.Context, string) ([]string, error) {
				return nil, errors.New("smembers failed")
			},
		}
		_, err := LoadSubscriberRoomsByChannel(t.Context(), mockCache, []string{"ch1"})
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

		result, err := LoadSubscriberRoomsByChannel(ctx, countingCache, []string{"ch1", "ch2", "ch1"})
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

	cache := newCheckerTestCacheClient(t)
	dedupService := dedup.NewService(cache, []int{5, 3, 1}, newCheckerTestLogger())
	checker := &YouTubeChecker{
		dedupService:        dedupService,
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

		require.NoError(t, dedupService.MarkAsNotified(ctx, stream.ID, start, 5))

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

		require.NoError(t, dedupService.MarkUpcomingEventNotified(ctx, "room1", "ch-live", stream))

		notifications, err = checker.buildLiveCatchupNotifications(ctx, "ch-live", stream, []string{"room1", "room2"}, now)
		require.NoError(t, err)
		require.Len(t, notifications, 1)
		assert.Equal(t, "room2", notifications[0].RoomID)
		assert.Equal(t, 5, notifications[0].MinutesUntil)

		require.NoError(t, dedupService.MarkAsNotified(ctx, stream.ID, start, 5))

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

func TestResolveEligibleLiveCatchupStartUsesRecentObservedAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	oldStart := now.Add(-(sharedconstants.LiveCatchupWindow + time.Hour))
	recentObserved := now.Add(-time.Minute)
	stream := &domain.Stream{
		ID:             "live-observed-recently",
		Status:         domain.StreamStatusLive,
		StartScheduled: &oldStart,
		StartActual:    &oldStart,
	}

	got, ok := resolveEligibleLiveCatchupStart(stream, now, &recentObserved)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, oldStart, *got)
}

func TestMergePersistedLiveSessionStreamsKeepsHolodexPrimaryFields(t *testing.T) {
	t.Parallel()

	channelID := "ch-merge"
	streamID := "stream-merge"
	holodexStart := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	persistedStart := time.Date(2026, 5, 14, 9, 59, 0, 0, time.UTC)
	lastSeenAt := time.Date(2026, 5, 14, 10, 4, 0, 0, time.UTC)
	streamsByChannel := map[string][]*domain.Stream{
		channelID: {{
			ID:             streamID,
			Title:          "Holodex title",
			ChannelID:      channelID,
			Status:         domain.StreamStatusLive,
			StartScheduled: &holodexStart,
			Channel:        &domain.Channel{ID: channelID, Name: "Holodex Channel"},
		}},
	}

	observed := mergePersistedLiveSessionStreams(streamsByChannel, []PersistedYouTubeLiveSession{{
		Stream: &domain.Stream{
			ID:             streamID,
			Title:          "DB title",
			ChannelID:      channelID,
			Status:         domain.StreamStatusLive,
			StartScheduled: &persistedStart,
			StartActual:    &persistedStart,
			Channel:        &domain.Channel{ID: channelID, Name: "DB Channel"},
		},
		LastSeenAt: lastSeenAt,
	}})

	require.Len(t, streamsByChannel[channelID], 1)
	got := streamsByChannel[channelID][0]
	assert.Equal(t, "Holodex title", got.Title)
	assert.Equal(t, holodexStart, *got.StartScheduled)
	assert.Equal(t, "Holodex Channel", got.Channel.Name)
	require.NotNil(t, got.StartActual)
	assert.Equal(t, persistedStart, *got.StartActual)
	assert.Equal(t, lastSeenAt, observed[streamID])
}

func TestMergePersistedLiveSessionStreamsDoesNotUseUpcomingObservedAtForLiveCatchup(t *testing.T) {
	t.Parallel()

	channelID := "ch-observed"
	streamID := "stream-observed"
	holodexStart := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	persistedScheduled := time.Date(2026, 5, 14, 10, 5, 0, 0, time.UTC)
	lastSeenAt := time.Date(2026, 5, 14, 10, 1, 0, 0, time.UTC)
	streamsByChannel := map[string][]*domain.Stream{
		channelID: {{
			ID:             streamID,
			Title:          "Holodex live",
			ChannelID:      channelID,
			Status:         domain.StreamStatusLive,
			StartScheduled: &holodexStart,
			Channel:        &domain.Channel{ID: channelID, Name: "Holodex Channel"},
		}},
	}

	observed := mergePersistedLiveSessionStreams(streamsByChannel, []PersistedYouTubeLiveSession{{
		Stream: &domain.Stream{
			ID:             streamID,
			Title:          "DB upcoming",
			ChannelID:      channelID,
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &persistedScheduled,
			Channel:        &domain.Channel{ID: channelID, Name: "DB Channel"},
		},
		LastSeenAt: lastSeenAt,
	}})

	require.Len(t, streamsByChannel[channelID], 1)
	assert.Empty(t, observed)
	assert.Nil(t, liveObservedAt(streamsByChannel[channelID][0], observed))
}

func TestMergePersistedLiveSessionStreamsPromotesHolodexUpcomingToPersistedLive(t *testing.T) {
	t.Parallel()

	channelID := "ch-promote"
	streamID := "stream-promote"
	holodexScheduled := time.Date(2026, 5, 14, 10, 5, 0, 0, time.UTC)
	persistedStart := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	lastSeenAt := time.Date(2026, 5, 14, 10, 1, 0, 0, time.UTC)
	streamsByChannel := map[string][]*domain.Stream{
		channelID: {{
			ID:             streamID,
			Title:          "Holodex stale upcoming",
			ChannelID:      channelID,
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &holodexScheduled,
			Channel:        &domain.Channel{ID: channelID, Name: "Holodex Channel"},
		}},
	}

	mergePersistedLiveSessionStreams(streamsByChannel, []PersistedYouTubeLiveSession{{
		Stream: &domain.Stream{
			ID:             streamID,
			Title:          "DB live",
			ChannelID:      channelID,
			Status:         domain.StreamStatusLive,
			StartScheduled: &persistedStart,
			StartActual:    &persistedStart,
			Channel:        &domain.Channel{ID: channelID, Name: "DB Channel"},
		},
		LastSeenAt: lastSeenAt,
	}})

	require.Len(t, streamsByChannel[channelID], 1)
	got := streamsByChannel[channelID][0]
	assert.Equal(t, domain.StreamStatusLive, got.Status)
	assert.Equal(t, holodexScheduled, *got.StartScheduled)
	require.NotNil(t, got.StartActual)
	assert.Equal(t, persistedStart, *got.StartActual)
	assert.Equal(t, "Holodex stale upcoming", got.Title)
}

func TestPersistedLiveGuardrailMetasSkipFreshObservationGrace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	session := PersistedYouTubeLiveSession{
		Stream: &domain.Stream{
			ID:        "live-fresh",
			ChannelID: "ch-live",
			Status:    domain.StreamStatusLive,
		},
		LastSeenAt: now.Add(-30 * time.Second),
	}

	metas := persistedLiveGuardrailMetas([]PersistedYouTubeLiveSession{session}, map[string][]string{
		"ch-live": {"room-1"},
	}, now)
	assert.Empty(t, metas)

	session.LastSeenAt = now.Add(-3 * time.Minute)
	metas = persistedLiveGuardrailMetas([]PersistedYouTubeLiveSession{session}, map[string][]string{
		"ch-live": {"room-1"},
	}, now)
	require.Len(t, metas, 1)
	assert.Equal(t, "live-fresh", metas[0].streamID)
}

func TestPersistedLiveGuardrailDoesNotStayInGraceWhenLastSeenKeepsRefreshing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 14, 10, 10, 0, 0, time.UTC)
	session := PersistedYouTubeLiveSession{
		Stream: &domain.Stream{
			ID:        "live-no-dispatch",
			ChannelID: "ch-1",
			Status:    domain.StreamStatusLive,
		},
		LastSeenAt:      now.Add(-30 * time.Second),
		LiveFirstSeenAt: now.Add(-5 * time.Minute),
	}

	metas := persistedLiveGuardrailMetas(
		[]PersistedYouTubeLiveSession{session},
		map[string][]string{"ch-1": {"room-1"}},
		now,
	)

	require.Len(t, metas, 1)
	assert.Equal(t, "live-no-dispatch", metas[0].streamID)
}

func TestYouTubeCheckerUsesDedupEvidenceWhenPgDispatchMissing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	checker, dedupService := newTestYouTubeCheckerWithDedup(t)
	source := &fakeYouTubeLiveSessionSource{}
	checker.persistedLiveSource = source

	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	require.NoError(t, dedupService.MarkAsNotified(ctx, "stream-dedup-evidence", start, 5))

	dispatched, err := checker.recentlyDispatchedOrNotifiedLiveStreamIDs(
		ctx,
		[]string{"stream-dedup-evidence"},
		start.Add(-24*time.Hour),
	)
	require.NoError(t, err)
	assert.Contains(t, dispatched, "stream-dedup-evidence")
}

func TestRecentLiveDispatchEvidenceIncludesSentRooms(t *testing.T) {
	t.Parallel()

	checker, _ := newTestYouTubeCheckerWithDedup(t)
	checker.persistedLiveSource = &fakeYouTubeLiveSessionSource{
		recentDispatch: map[string]bool{"stream-delivery": true},
		recentSentRooms: map[string][]string{
			"stream-delivery": {"room-1"},
		},
	}

	evidence, err := checker.recentLiveDispatchEvidence(
		t.Context(),
		[]string{"stream-delivery"},
		time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC).Add(-24*time.Hour),
	)
	require.NoError(t, err)
	assert.Contains(t, evidence.pgDispatchedStreamIDs, "stream-delivery")
	assert.Contains(t, evidence.sentRoomsByStreamID["stream-delivery"], "room-1")
	assert.Equal(t, []string{"room-2"}, missingLiveDeliveryRooms([]string{"room-1", "room-2"}, evidence.sentRoomsByStreamID["stream-delivery"]))
}

func TestRecentLiveDispatchEvidenceContinuesWhenDeliveryLookupFailsButDispatchEvidenceExists(t *testing.T) {
	t.Parallel()

	checker, dedupService := newTestYouTubeCheckerWithDedup(t)
	source := &fakeYouTubeLiveSessionSource{
		recentDispatch:     map[string]bool{"stream-delivery-error": true},
		recentSentRoomsErr: errors.New("sent delivery lookup failed"),
	}
	checker.persistedLiveSource = source

	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	require.NoError(t, dedupService.MarkAsNotified(t.Context(), "stream-delivery-error", start, 5))

	evidence, err := checker.recentLiveDispatchEvidence(
		t.Context(),
		[]string{"stream-delivery-error"},
		start.Add(-24*time.Hour),
	)
	require.NoError(t, err)
	assert.True(t, evidence.deliveryCheckFailed)
	assert.Contains(t, evidence.pgDispatchedStreamIDs, "stream-delivery-error")
}

func TestRecentLiveDispatchEvidenceContinuesWhenDeliveryLookupFailsButValkeyEvidenceExists(t *testing.T) {
	t.Parallel()

	checker, dedupService := newTestYouTubeCheckerWithDedup(t)
	checker.persistedLiveSource = &fakeYouTubeLiveSessionSource{
		recentSentRoomsErr: errors.New("sent delivery lookup failed"),
	}

	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	require.NoError(t, dedupService.MarkAsNotified(t.Context(), "stream-valkey-evidence", start, 5))

	evidence, err := checker.recentLiveDispatchEvidence(
		t.Context(),
		[]string{"stream-valkey-evidence"},
		start.Add(-24*time.Hour),
	)
	require.NoError(t, err)
	assert.Contains(t, evidence.valkeyNotifiedStreamIDs, "stream-valkey-evidence")
}

func TestRecentLiveDispatchEvidenceReturnsDeliveryLookupErrorWithoutEvidence(t *testing.T) {
	t.Parallel()

	checker, _ := newTestYouTubeCheckerWithDedup(t)
	checker.persistedLiveSource = &fakeYouTubeLiveSessionSource{
		recentSentRoomsErr: errors.New("sent delivery lookup failed"),
	}

	_, err := checker.recentLiveDispatchEvidence(
		t.Context(),
		[]string{"stream-without-evidence"},
		time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC).Add(-24*time.Hour),
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "pg sent delivery evidence")
	assert.ErrorContains(t, err, "sent delivery lookup failed")
}

func TestLiveCatchupSuppressesRoomsAfterPublishedMarker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	start := now.Add(-2 * time.Minute)

	checker, dedupService := newTestYouTubeCheckerWithDedup(t)

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

	require.NoError(t, dedupService.MarkAsNotified(ctx, stream.ID, start, 5))
	require.NoError(t, dedupService.MarkUpcomingEventNotified(ctx, "room1", "ch-live", stream))

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

	checker, dedupService := newTestYouTubeCheckerWithDedup(t)
	require.NoError(t, dedupService.MarkAsNotified(ctx, "live-rescheduled", oldStart, 5))

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

	cache := newCheckerTestCacheClient(t)
	dedupService := dedup.NewService(cache, []int{5, 3, 1}, newCheckerTestLogger())
	checker := &YouTubeChecker{
		dedupService:        dedupService,
		targetPolicy:        sharedchecker.NewTargetMinutePolicy([]int{5, 3, 1}),
		evaluationWindowCap: 75 * time.Second,
		logger:              newCheckerTestLogger(),
	}
	return checker, dedupService
}

var _ cache.Client = (*cachemocks.Client)(nil)
