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
	lastNotifiedAt   *time.Time
	expectedInterval time.Duration
}

func computeNextCheckAtCases(now time.Time) []computeNextCheckAtCase {
	notified5m := now.Add(-5 * time.Minute)
	notified20m := now.Add(-20 * time.Minute)
	past5m := now.Add(-5 * time.Minute)
	tier1mid := now.Add(22 * time.Minute)
	tier2mid := now.Add(tier1Window + 10*time.Minute)
	tier3mid := now.Add(tier2Window + 1*time.Hour)
	tier4mid := now.Add(tier3Window + 1*time.Hour)

	return []computeNextCheckAtCase{
		{
			name:             "예정 없음, 최근 알림 없음 -> NoUpcomingInterval",
			nearestStart:     nil,
			lastNotifiedAt:   nil,
			expectedInterval: noUpcomingInterval,
		},
		{
			name:             "예정 없음, 5분 전 알림 -> NoUpcomingInterval",
			nearestStart:     nil,
			lastNotifiedAt:   &notified5m,
			expectedInterval: noUpcomingInterval,
		},
		{
			name:             "예정 없음, 20분 전 알림 -> NoUpcomingInterval (윈도우 경과)",
			nearestStart:     nil,
			lastNotifiedAt:   &notified20m,
			expectedInterval: noUpcomingInterval,
		},
		{
			name:             "이미 지난 시작 시각 -> Tier1",
			nearestStart:     &past5m,
			lastNotifiedAt:   nil,
			expectedInterval: tier1Interval,
		},
		{
			name:             "Tier1: 22분 후 예정 -> Tier1Interval",
			nearestStart:     &tier1mid,
			lastNotifiedAt:   nil,
			expectedInterval: tier1Interval,
		},
		{
			name:             "Tier2: Tier1Window + 10분 후 -> Tier2Interval",
			nearestStart:     &tier2mid,
			lastNotifiedAt:   nil,
			expectedInterval: tier2Interval,
		},
		{
			name:             "Tier3: Tier2Window + 1시간 후 -> Tier3Interval",
			nearestStart:     &tier3mid,
			lastNotifiedAt:   nil,
			expectedInterval: tier3Interval,
		},
		{
			name:             "Tier4: Tier3Window + 1시간 후 -> Tier4Interval",
			nearestStart:     &tier4mid,
			lastNotifiedAt:   nil,
			expectedInterval: tier4Interval,
		},
	}
}

func TestComputeNextCheckAt(t *testing.T) {
	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)

	for _, tc := range computeNextCheckAtCases(now) {
		t.Run(tc.name, func(t *testing.T) {
			result := ComputeNextCheckAt(now, tc.nearestStart, tc.lastNotifiedAt)
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

func TestSelectDueChannels_ForceDueChannels(t *testing.T) {
	ts := newTestScheduler()

	ts.fullRefreshAt = time.Now().Add(1 * time.Hour)

	ts.states[testChannelIDA] = &channelScheduleState{
		nextCheckAt: time.Now().Add(1 * time.Hour),
		forceDue:    true,
	}

	due := ts.SelectDueChannels(time.Now(), []string{testChannelIDA})
	assert.Len(t, due, 1)
}

func TestSelectDueChannels_FutureNextCheckAtNotDue(t *testing.T) {
	ts := newTestScheduler()

	ts.fullRefreshAt = time.Now().Add(1 * time.Hour)

	ts.states[testChannelIDA] = &channelScheduleState{
		nextCheckAt: time.Now().Add(1 * time.Hour),
		forceDue:    false,
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
			forceDue:    false,
		}
	}

	due := ts.SelectDueChannels(time.Now(), ids)
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

	ts.UpdateChannelState(time.Now(), testChannelIDA, []*domain.Stream{stream})

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	st, ok := ts.states[testChannelIDA]
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

	ts.states[testChannelIDA] = &channelScheduleState{
		lastNotifiedAt: &notifiedAt,
	}
	ts.mu.Unlock()

	ts.UpdateChannelState(time.Now(), testChannelIDA, nil)

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	st := ts.states[testChannelIDA]
	require.NotNil(t, st.lastNotifiedAt)
	assert.Equal(t, notifiedAt, *st.lastNotifiedAt)
}

func TestMarkChannelDue_ThenSelectReturnsIt(t *testing.T) {
	ts := newTestScheduler()

	ts.fullRefreshAt = time.Now().Add(1 * time.Hour)

	ts.states[testChannelIDA] = &channelScheduleState{
		nextCheckAt: time.Now().Add(1 * time.Hour),
		forceDue:    false,
	}

	assert.Empty(t, ts.SelectDueChannels(time.Now(), []string{testChannelIDA}))

	ts.MarkChannelDue(testChannelIDA)

	due := ts.SelectDueChannels(time.Now(), []string{testChannelIDA})
	assert.Len(t, due, 1)
}

func TestMarkRecentlyNotified_AffectsCompute(t *testing.T) {
	ts := newTestScheduler()
	ts.MarkChannelRecentlyNotified(testChannelIDA)

	ts.mu.RLock()

	st, ok := ts.states[testChannelIDA]
	require.True(t, ok)
	require.NotNil(t, st)

	lastNotified := st.lastNotifiedAt

	ts.mu.RUnlock()

	// 예정 없음 + 최근 알림도 NoUpcomingInterval보다 느려지지 않는다.
	now := time.Now()
	result := ComputeNextCheckAt(now, nil, lastNotified)
	assert.Equal(t, now.Add(noUpcomingInterval), result)
}

// 정렬 tick이 깨어나는 지연이 줄어드는 경우에도 Tier1 채널은 매 tick due여야 한다.
// 다음 점검 시각이 처리 완료 시각이나 이전 tick의 늦은 깨어남에 끌려가면 한 tick을 건너뛴다.
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

// 허용치는 깨어남 지연만 흡수하고, 3분 간격 채널을 한 tick 앞당겨 점검하지 않는다.
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

func TestForgetChannel(t *testing.T) {
	ts := newTestScheduler()

	ts.states[testChannelIDA] = &channelScheduleState{}

	ts.ForgetChannel(testChannelIDA)

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	_, ok := ts.states[testChannelIDA]
	assert.False(t, ok)
}
