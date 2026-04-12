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
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// channelScheduleState: 채널별 Tier 스케줄 상태
type channelScheduleState struct {
	nextCheckAt      time.Time
	lastCheckedAt    time.Time
	nearestStartAt   *time.Time
	forceDue         bool
	lastStreamsCount int
	lastNotifiedAt   *time.Time
}

func (s *channelScheduleState) isDue(now time.Time) bool {
	return s.forceDue || !now.Before(s.nextCheckAt)
}

type TieredScheduler struct {
	mu            sync.RWMutex
	states        map[string]*channelScheduleState
	fullRefreshAt time.Time
	logger        *slog.Logger
}

func NewTieredScheduler(logger *slog.Logger) *TieredScheduler {
	return &TieredScheduler{
		states:        make(map[string]*channelScheduleState),
		fullRefreshAt: time.Time{}, // zero value -> 첫 호출 시 즉시 full refresh
		logger:        logger,
	}
}

// fullRefreshAt이 지나면 모든 채널을 반환하고 타이머를 재설정한다.
func (ts *TieredScheduler) SelectDueChannels(channelIDs []string) []string {
	now := time.Now()

	ts.mu.Lock()
	forceAll := false
	if !now.Before(ts.fullRefreshAt) {
		ts.fullRefreshAt = now.Add(constants.FullRefreshInterval)
		forceAll = true
	}
	ts.mu.Unlock()

	if forceAll {
		result := make([]string, len(channelIDs))
		copy(result, channelIDs)
		return result
	}

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	due := make([]string, 0, len(channelIDs))
	for _, id := range channelIDs {
		st, exists := ts.states[id]
		if !exists || st.isDue(now) {
			due = append(due, id)
		}
	}

	ts.logger.Debug("Tier gating applied",
		slog.Int("all_channels", len(channelIDs)),
		slog.Int("due_channels", len(due)),
	)

	return due
}

func (ts *TieredScheduler) UpdateChannelState(channelID string, streams []*domain.Stream) {
	now := time.Now()

	// 미래 예정 방송 중 가장 가까운 시작 시각 탐색
	var nearest *time.Time
	for _, s := range streams {
		if s.IsUpcoming() && s.StartScheduled != nil && s.StartScheduled.After(now) {
			if nearest == nil || s.StartScheduled.Before(*nearest) {
				t := *s.StartScheduled
				nearest = &t
			}
		}
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	// 기존 lastNotifiedAt 보존
	var lastNotifiedAt *time.Time
	if existing, ok := ts.states[channelID]; ok {
		lastNotifiedAt = existing.lastNotifiedAt
	}

	nextAt := ComputeNextCheckAt(nearest, lastNotifiedAt)

	ts.states[channelID] = &channelScheduleState{
		nextCheckAt:      nextAt,
		lastCheckedAt:    now,
		nearestStartAt:   nearest,
		forceDue:         false,
		lastStreamsCount: len(streams),
		lastNotifiedAt:   lastNotifiedAt,
	}

	ts.logger.Debug("Channel schedule state updated",
		slog.String("channel_id", channelID),
		slog.Time("next_at", nextAt),
		slog.Bool("has_nearest", nearest != nil),
		slog.Int("streams", len(streams)),
	)
}

func (ts *TieredScheduler) LastCheckedAt(channelID string) time.Time {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	st, ok := ts.states[channelID]
	if !ok {
		return time.Time{}
	}

	return st.lastCheckedAt
}

func (ts *TieredScheduler) MarkChannelDue(channelID string) {
	now := time.Now()

	ts.mu.Lock()
	defer ts.mu.Unlock()

	if st, ok := ts.states[channelID]; ok {
		st.forceDue = true
		st.nextCheckAt = now
	} else {
		ts.states[channelID] = &channelScheduleState{
			nextCheckAt:   now,
			lastCheckedAt: time.Time{},
			forceDue:      true,
		}
	}

	ts.logger.Debug("Channel marked due", slog.String("channel_id", channelID))
}

func (ts *TieredScheduler) MarkChannelRecentlyNotified(channelID string) {
	now := time.Now()

	ts.mu.Lock()
	defer ts.mu.Unlock()

	if st, ok := ts.states[channelID]; ok {
		st.lastNotifiedAt = &now
	} else {
		ts.states[channelID] = &channelScheduleState{
			nextCheckAt:    now,
			lastCheckedAt:  time.Time{},
			lastNotifiedAt: &now,
		}
	}
}

func (ts *TieredScheduler) ForgetChannel(channelID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	delete(ts.states, channelID)
	ts.logger.Debug("Channel schedule state forgotten", slog.String("channel_id", channelID))
}

func (ts *TieredScheduler) ChannelCount() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.states)
}
