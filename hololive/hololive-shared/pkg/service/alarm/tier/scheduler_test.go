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

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func newTestScheduler() *TieredScheduler {
	return NewTieredScheduler(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

func TestComputeNextCheckAt(t *testing.T) {
	tolerance := 2 * time.Second

	now := time.Now()
	notified5m := now.Add(-5 * time.Minute)
	notified20m := now.Add(-20 * time.Minute)
	past5m := now.Add(-5 * time.Minute)
	tier1mid := now.Add(22 * time.Minute)
	tier2mid := now.Add(constants.Tier1Window + 10*time.Minute)
	tier3mid := now.Add(constants.Tier2Window + 1*time.Hour)
	tier4mid := now.Add(constants.Tier3Window + 1*time.Hour)

	tests := []struct {
		name             string
		nearestStart     *time.Time
		lastNotifiedAt   *time.Time
		expectedInterval time.Duration
	}{
		{
			name:             "예정 없음, 최근 알림 없음 -> NoUpcomingInterval",
			nearestStart:     nil,
			lastNotifiedAt:   nil,
			expectedInterval: constants.NoUpcomingInterval,
		},
		{
			name:             "예정 없음, 5분 전 알림 -> Tier2Interval (recently notified)",
			nearestStart:     nil,
			lastNotifiedAt:   &notified5m,
			expectedInterval: constants.Tier2Interval,
		},
		{
			name:             "예정 없음, 20분 전 알림 -> NoUpcomingInterval (윈도우 경과)",
			nearestStart:     nil,
			lastNotifiedAt:   &notified20m,
			expectedInterval: constants.NoUpcomingInterval,
		},
		{
			name:             "이미 지난 시작 시각 -> Tier1",
			nearestStart:     &past5m,
			lastNotifiedAt:   nil,
			expectedInterval: constants.Tier1Interval,
		},
		{
			name:             "Tier1: 22분 후 예정 -> Tier1Interval",
			nearestStart:     &tier1mid,
			lastNotifiedAt:   nil,
			expectedInterval: constants.Tier1Interval,
		},
		{
			name:             "Tier2: Tier1Window + 10분 후 -> Tier2Interval",
			nearestStart:     &tier2mid,
			lastNotifiedAt:   nil,
			expectedInterval: constants.Tier2Interval,
		},
		{
			name:             "Tier3: Tier2Window + 1시간 후 -> Tier3Interval",
			nearestStart:     &tier3mid,
			lastNotifiedAt:   nil,
			expectedInterval: constants.Tier3Interval,
		},
		{
			name:             "Tier4: Tier3Window + 1시간 후 -> Tier4Interval",
			nearestStart:     &tier4mid,
			lastNotifiedAt:   nil,
			expectedInterval: constants.Tier4Interval,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ComputeNextCheckAt(tc.nearestStart, tc.lastNotifiedAt)
			expected := time.Now().Add(tc.expectedInterval)
			diff := result.Sub(expected)
			if diff < 0 {
				diff = -diff
			}
			assert.Less(t, diff, tolerance, "expected ~%v from now, got diff=%v", tc.expectedInterval, diff)
		})
	}
}

func TestSelectDueChannels_UnknownChannelsAreDue(t *testing.T) {
	ts := newTestScheduler()
	// full_refresh_at을 미래로 설정하여 forceAll 방지
	ts.fullRefreshAt = time.Now().Add(1 * time.Hour)

	ids := []string{"UC_A", "UC_B"}
	due := ts.SelectDueChannels(ids)
	assert.Len(t, due, 2)
}

func TestSelectDueChannels_ForceDueChannels(t *testing.T) {
	ts := newTestScheduler()
	ts.fullRefreshAt = time.Now().Add(1 * time.Hour)

	ts.states["UC_A"] = &channelScheduleState{
		nextCheckAt: time.Now().Add(1 * time.Hour),
		forceDue:    true,
	}

	due := ts.SelectDueChannels([]string{"UC_A"})
	assert.Len(t, due, 1)
}

func TestSelectDueChannels_FutureNextCheckAtNotDue(t *testing.T) {
	ts := newTestScheduler()
	ts.fullRefreshAt = time.Now().Add(1 * time.Hour)

	ts.states["UC_A"] = &channelScheduleState{
		nextCheckAt: time.Now().Add(1 * time.Hour),
		forceDue:    false,
	}

	due := ts.SelectDueChannels([]string{"UC_A"})
	assert.Empty(t, due)
}

func TestSelectDueChannels_FullRefreshReturnsAll(t *testing.T) {
	ts := newTestScheduler()
	// fullRefreshAt은 zero value -> 즉시 만료

	ids := []string{"UC_A", "UC_B", "UC_C"}
	for _, id := range ids {
		ts.states[id] = &channelScheduleState{
			nextCheckAt: time.Now().Add(1 * time.Hour),
			forceDue:    false,
		}
	}

	due := ts.SelectDueChannels(ids)
	assert.Len(t, due, 3)
}

func TestUpdateChannelState_SetsNearestStart(t *testing.T) {
	ts := newTestScheduler()
	start := time.Now().Add(2 * time.Hour)
	stream := &domain.Stream{
		ID:             "vid",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &start,
	}

	ts.UpdateChannelState("UC_A", []*domain.Stream{stream})

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	st, ok := ts.states["UC_A"]
	require.True(t, ok)
	require.NotNil(t, st.nearestStartAt)
	diff := st.nearestStartAt.Sub(start)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff, time.Second)
}

func TestUpdateChannelState_PreservesLastNotifiedAt(t *testing.T) {
	ts := newTestScheduler()
	notifiedAt := time.Now().Add(-3 * time.Minute)

	ts.mu.Lock()
	ts.states["UC_A"] = &channelScheduleState{
		lastNotifiedAt: &notifiedAt,
	}
	ts.mu.Unlock()

	ts.UpdateChannelState("UC_A", nil)

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	st := ts.states["UC_A"]
	require.NotNil(t, st.lastNotifiedAt)
	assert.Equal(t, notifiedAt, *st.lastNotifiedAt)
}

func TestMarkChannelDue_ThenSelectReturnsIt(t *testing.T) {
	ts := newTestScheduler()
	ts.fullRefreshAt = time.Now().Add(1 * time.Hour)

	ts.states["UC_A"] = &channelScheduleState{
		nextCheckAt: time.Now().Add(1 * time.Hour),
		forceDue:    false,
	}

	assert.Empty(t, ts.SelectDueChannels([]string{"UC_A"}))

	ts.MarkChannelDue("UC_A")

	due := ts.SelectDueChannels([]string{"UC_A"})
	assert.Len(t, due, 1)
}

func TestMarkRecentlyNotified_AffectsCompute(t *testing.T) {
	ts := newTestScheduler()
	ts.MarkChannelRecentlyNotified("UC_A")

	ts.mu.RLock()
	st := ts.states["UC_A"]
	lastNotified := st.lastNotifiedAt
	ts.mu.RUnlock()

	// 예정 없음 + 최근 알림 -> Tier2Interval
	result := ComputeNextCheckAt(nil, lastNotified)
	expected := time.Now().Add(constants.Tier2Interval)
	diff := result.Sub(expected)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff, 2*time.Second)
}

func TestForgetChannel(t *testing.T) {
	ts := newTestScheduler()
	ts.states["UC_A"] = &channelScheduleState{}

	ts.ForgetChannel("UC_A")

	ts.mu.RLock()
	defer ts.mu.RUnlock()
	_, ok := ts.states["UC_A"]
	assert.False(t, ok)
}
