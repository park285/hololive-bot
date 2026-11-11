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

package tier

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func newTestScheduler() *TieredScheduler {
	return NewTieredScheduler(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

type computeNextCheckAtCase struct {
	name             string
	nearestStart     *time.Time
	expectedInterval time.Duration
}

func computeNextCheckAtCases(now time.Time) []computeNextCheckAtCase {
	past5m := now.Add(-5 * time.Minute)
	tier1mid := now.Add(22 * time.Minute)
	tier2mid := now.Add(tier1Window + 10*time.Minute)
	tier3mid := now.Add(tier2Window + 1*time.Hour)
	tier4mid := now.Add(tier3Window + 1*time.Hour)

	return []computeNextCheckAtCase{
		{
			name:             "예정 없음 -> NoUpcomingInterval",
			nearestStart:     nil,
			expectedInterval: noUpcomingInterval,
		},
		{
			name:             "이미 지난 시작 시각 -> Tier1",
			nearestStart:     &past5m,
			expectedInterval: tier1Interval,
		},
		{
			name:             "Tier1: 22분 후 예정 -> Tier1Interval",
			nearestStart:     &tier1mid,
			expectedInterval: tier1Interval,
		},
		{
			name:             "Tier2: Tier1Window + 10분 후 -> Tier2Interval",
			nearestStart:     &tier2mid,
			expectedInterval: tier2Interval,
		},
		{
			name:             "Tier3: Tier2Window + 1시간 후 -> Tier3Interval",
			nearestStart:     &tier3mid,
			expectedInterval: tier3Interval,
		},
		{
			name:             "Tier4: Tier3Window + 1시간 후 -> Tier4Interval",
			nearestStart:     &tier4mid,
			expectedInterval: tier4Interval,
		},
	}
}

func TestComputeNextCheckAt(t *testing.T) {
	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)

	for _, tc := range computeNextCheckAtCases(now) {
		t.Run(tc.name, func(t *testing.T) {
			result := ComputeNextCheckAt(now, tc.nearestStart)
			assert.Equal(t, now.Add(tc.expectedInterval), result)
		})
	}
}

func TestSelectDueChannels_UnknownChannelsAreDue(t *testing.T) {
	ts := newTestScheduler()
	// full_refresh_at을 미래로 설정하여 forceAll 방지
	ts.fullRefreshAt = time.Now().Add(1 * time.Hour)

	ids := []string{testChannelIDA, "UC_B"}
	due := ts.SelectDueChannels(time.Now(), ids)
	assert.Len(t, due, 2)
}

func TestSelectDueChannels_FutureNextCheckAtNotDue(t *testing.T) {
	ts := newTestScheduler()

	ts.fullRefreshAt = time.Now().Add(1 * time.Hour)

	ts.states[testChannelIDA] = &channelScheduleState{
		nextCheckAt: time.Now().Add(1 * time.Hour),
	}

	due := ts.SelectDueChannels(time.Now(), []string{testChannelIDA})
	assert.Empty(t, due)
}

func TestSelectDueChannels_FullRefreshReturnsAll(t *testing.T) {
	ts := newTestScheduler()
	// fullRefreshAt은 zero value -> 즉시 만료

	ids := []string{testChannelIDA, "UC_B", "UC_C"}
	for _, id := range ids {
		ts.states[id] = &channelScheduleState{
			nextCheckAt: time.Now().Add(1 * time.Hour),
		}
	}

	due := ts.SelectDueChannels(time.Now(), ids)
	assert.Len(t, due, 3)
}

func TestUpdateChannelState_SchedulesByNearestUpcomingStart(t *testing.T) {
	ts := newTestScheduler()
	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	nearest := now.Add(2 * time.Hour)
	later := now.Add(tier3Window + time.Hour)
	streams := []*domain.Stream{
		{ID: "later", Status: domain.StreamStatusUpcoming, StartScheduled: &later},
		{ID: "nearest", Status: domain.StreamStatusUpcoming, StartScheduled: &nearest},
	}

	ts.UpdateChannelState(now, testChannelIDA, streams)

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	st, ok := ts.states[testChannelIDA]
	require.True(t, ok)
	assert.Equal(t, now.Add(tier2Interval), st.nextCheckAt)
	assert.Equal(t, now, st.lastCheckedAt)
}

func TestSelectDueChannels_Tier1DueEveryAlignedTickDespiteWakeJitter(t *testing.T) {
	ts := newTestScheduler()
	base := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)

	ts.fullRefreshAt = base.Add(time.Hour)

	start := base.Add(10 * time.Minute)
	streams := []*domain.Stream{{ID: "vid", Status: domain.StreamStatusUpcoming, StartScheduled: &start}}
	wakeDelays := []time.Duration{900 * time.Millisecond, 400 * time.Millisecond, 5 * time.Millisecond, 300 * time.Millisecond}

	for tick, delay := range wakeDelays {
		now := base.Add(time.Duration(tick)*time.Minute + delay)

		due := ts.SelectDueChannels(now, []string{testChannelIDA})
		require.Equal(t, []string{testChannelIDA}, due, "tick %d", tick)

		// 평가가 오래 걸려도 다음 점검 시각은 iteration 시각 기준이다.
		ts.UpdateChannelState(now, testChannelIDA, streams)
		assert.Equal(t, now, ts.LastCheckedAt(testChannelIDA))
	}
}

func TestSelectDueChannels_SlackDoesNotAdvanceLongerTierByOneTick(t *testing.T) {
	ts := newTestScheduler()
	base := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)

	ts.fullRefreshAt = base.Add(time.Hour)

	start := base.Add(tier1Window + 10*time.Minute)
	streams := []*domain.Stream{{ID: "vid", Status: domain.StreamStatusUpcoming, StartScheduled: &start}}
	ts.UpdateChannelState(base, testChannelIDA, streams)

	assert.Empty(t, ts.SelectDueChannels(base.Add(time.Minute), []string{testChannelIDA}))
	assert.Empty(t, ts.SelectDueChannels(base.Add(2*time.Minute), []string{testChannelIDA}))
	assert.Equal(t, []string{testChannelIDA}, ts.SelectDueChannels(base.Add(tier2Interval-time.Millisecond), []string{testChannelIDA}))
}
