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

package polltarget

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTargetsWithCacheValidation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)

	t.Run("accepts non cache candidate without db validation", func(t *testing.T) {
		t.Parallel()

		refresher := &youTubePollTargetRefresher{
			loadAlarmChannelIDs: func(context.Context) ([]string, error) {
				t.Fatal("loadAlarmChannelIDs must not be called")
				return nil, nil
			},
		}

		targets, ok := refresher.resolveTargetsWithCacheValidation(
			t.Context(),
			now,
			testValidationOperationalChannels("UC_NOTIFY", "UC_STATS"),
			[]string{"UC_NOTIFY"},
			false,
		)

		require.True(t, ok)
		assert.Equal(t, []string{"UC_NOTIFY"}, targets.NotificationChannelIDs)
		assert.Equal(t, []string{"UC_NOTIFY", "UC_STATS"}, targets.StatsChannelIDs)
		assert.Nil(t, refresher.cacheOnlyFirstSeen)
	})

	t.Run("allows pending cache only addition during grace period", func(t *testing.T) {
		t.Parallel()

		dbCalls := 0
		refresher := &youTubePollTargetRefresher{
			lastResolvedTargets: youtubePollTargets{NotificationChannelIDs: []string{"UC_BASE"}},
			loadAlarmChannelIDs: func(context.Context) ([]string, error) {
				dbCalls++
				return []string{"UC_BASE"}, nil
			},
		}

		targets, ok := refresher.resolveTargetsWithCacheValidation(
			t.Context(),
			now,
			testValidationOperationalChannels("UC_BASE", "UC_CACHE_ONLY"),
			[]string{"UC_BASE", "UC_CACHE_ONLY"},
			true,
		)

		require.True(t, ok)
		assert.Equal(t, []string{"UC_BASE", "UC_CACHE_ONLY"}, targets.NotificationChannelIDs)
		assert.Equal(t, []string{"UC_BASE", "UC_CACHE_ONLY"}, targets.StatsChannelIDs)
		assert.Equal(t, 1, dbCalls)
		assert.Equal(t, now, refresher.cacheOnlyFirstSeen["UC_CACHE_ONLY"])
	})

	t.Run("drops expired cache only addition without db validation", func(t *testing.T) {
		t.Parallel()

		dbCalled := false
		refresher := &youTubePollTargetRefresher{
			lastResolvedTargets: youtubePollTargets{NotificationChannelIDs: []string{"UC_BASE", "UC_CACHE_ONLY"}},
			cacheOnlyFirstSeen: map[string]time.Time{
				"UC_CACHE_ONLY": now.Add(-youtubePollTargetCacheOnlyAdditionGracePeriod - time.Nanosecond),
			},
			loadAlarmChannelIDs: func(context.Context) ([]string, error) {
				dbCalled = true
				return nil, nil
			},
		}

		targets, ok := refresher.resolveTargetsWithCacheValidation(
			t.Context(),
			now,
			testValidationOperationalChannels("UC_BASE", "UC_CACHE_ONLY"),
			[]string{"UC_BASE", "UC_CACHE_ONLY"},
			true,
		)

		require.True(t, ok)
		assert.Equal(t, []string{"UC_BASE"}, targets.NotificationChannelIDs)
		assert.False(t, dbCalled)
	})

	t.Run("validates cache removals against db", func(t *testing.T) {
		t.Parallel()

		dbCalls := 0
		refresher := &youTubePollTargetRefresher{
			lastResolvedTargets: youtubePollTargets{NotificationChannelIDs: []string{"UC_BASE", "UC_REMOVED"}},
			loadAlarmChannelIDs: func(context.Context) ([]string, error) {
				dbCalls++
				return []string{"UC_BASE", "UC_REMOVED"}, nil
			},
		}

		targets, ok := refresher.resolveTargetsWithCacheValidation(
			t.Context(),
			now,
			testValidationOperationalChannels("UC_BASE", "UC_REMOVED"),
			[]string{"UC_BASE"},
			true,
		)

		require.True(t, ok)
		assert.Equal(t, []string{"UC_BASE", "UC_REMOVED"}, targets.NotificationChannelIDs)
		assert.Equal(t, 1, dbCalls)
	})

	t.Run("fails closed when db validation fails", func(t *testing.T) {
		t.Parallel()

		refresher := &youTubePollTargetRefresher{
			lastResolvedTargets: youtubePollTargets{NotificationChannelIDs: []string{"UC_PREVIOUS"}},
			loadAlarmChannelIDs: func(context.Context) ([]string, error) {
				return nil, assert.AnError
			},
		}

		targets, ok := refresher.resolveTargetsWithCacheValidation(
			t.Context(),
			now,
			testValidationOperationalChannels("UC_PREVIOUS"),
			nil,
			true,
		)

		require.False(t, ok)
		assert.Empty(t, targets.NotificationChannelIDs)
		assert.Empty(t, targets.StatsChannelIDs)
	})
}

func TestCacheOnlyValidationHelpersHandleNilAndEmptyInputs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)

	assert.NotPanics(t, func() {
		trackCacheOnlyAdditions(now, []string{"UC_NEW"}, nil)
	})
	assert.NotPanics(t, func() {
		clearExpiredOrResolvedCacheOnly(nil, []string{"UC_NEW"}, []string{"UC_NEW"})
	})

	allowed, expired := filterGracefulCacheOnlyAdditions(now, []string{"UC_NEW"}, nil, time.Minute)
	assert.Nil(t, allowed)
	assert.Nil(t, expired)
	assert.False(t, hasPendingCacheOnlyValidation(now, nil, map[string]time.Time{"UC_NEW": now}, time.Minute))
	assert.False(t, hasPendingCacheOnlyValidation(now, []string{"UC_NEW"}, nil, time.Minute))
}

func TestCacheOnlyValidationHelpersTrackAndExpire(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	expiredAt := now.Add(-youtubePollTargetCacheOnlyAdditionGracePeriod - time.Nanosecond)
	state := map[string]time.Time{
		"UC_EXISTING": expiredAt,
	}

	trackCacheOnlyAdditions(now, []string{"", "UC_EXISTING", "UC_NEW"}, state)

	assert.NotContains(t, state, "")
	assert.Equal(t, expiredAt, state["UC_EXISTING"])
	assert.Equal(t, now, state["UC_NEW"])

	allowed, expired := filterGracefulCacheOnlyAdditions(
		now,
		[]string{"UC_EXISTING", "UC_NEW", "UC_MISSING"},
		state,
		youtubePollTargetCacheOnlyAdditionGracePeriod,
	)

	assert.Equal(t, []string{"UC_NEW"}, allowed)
	assert.Equal(t, []string{"UC_EXISTING"}, expired)
	assert.True(t, hasPendingCacheOnlyValidation(
		now,
		[]string{"UC_NEW"},
		state,
		youtubePollTargetCacheOnlyAdditionGracePeriod,
	))
	assert.False(t, hasPendingCacheOnlyValidation(
		now,
		[]string{"UC_EXISTING"},
		state,
		youtubePollTargetCacheOnlyAdditionGracePeriod,
	))
}

func TestClearExpiredOrResolvedCacheOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	state := map[string]time.Time{
		"UC_RESOLVED": now,
		"UC_MISSING":  now,
		"UC_PENDING":  now,
	}

	clearExpiredOrResolvedCacheOnly(state, []string{"UC_RESOLVED"}, []string{"UC_RESOLVED", "UC_PENDING"})

	assert.Equal(t, map[string]time.Time{"UC_PENDING": now}, state)
}

func testValidationOperationalChannels(channelIDs ...string) []communityShortsOperationalChannel {
	channels := make([]communityShortsOperationalChannel, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		channels = append(channels, communityShortsOperationalChannel{
			ChannelID: channelID,
			Enabled:   true,
		})
	}
	return channels
}
