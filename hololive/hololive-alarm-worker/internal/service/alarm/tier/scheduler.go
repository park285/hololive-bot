package tier

import (
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const dueCheckSlack = 15 * time.Second

func reachedWithSlack(now, at time.Time) bool {
	return !now.Add(dueCheckSlack).Before(at)
}

type channelScheduleState struct {
	nextCheckAt   time.Time
	lastCheckedAt time.Time
}

func (s *channelScheduleState) isDue(now time.Time) bool {
	return reachedWithSlack(now, s.nextCheckAt)
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
		fullRefreshAt: time.Time{},
		logger:        logger,
	}
}

func (ts *TieredScheduler) SelectDueChannels(now time.Time, channelIDs []string) []string {
	ts.mu.Lock()

	forceAll := false

	if reachedWithSlack(now, ts.fullRefreshAt) {
		ts.fullRefreshAt = now.Add(fullRefreshInterval)
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

func (ts *TieredScheduler) UpdateChannelState(now time.Time, channelID string, streams []*domain.Stream) {
	nearest := nearestUpcomingStart(streams, now)

	nextAt := ComputeNextCheckAt(now, nearest)

	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.states[channelID] = &channelScheduleState{
		nextCheckAt:   nextAt,
		lastCheckedAt: now,
	}

	ts.logger.Debug("Channel schedule state updated",
		slog.String("channel_id", channelID),
		slog.Time("next_at", nextAt),
		slog.Bool("has_nearest", nearest != nil),
		slog.Int("streams", len(streams)),
	)
}

func nearestUpcomingStart(streams []*domain.Stream, now time.Time) *time.Time {
	var nearest *time.Time

	for _, s := range streams {
		if !streamStartsAfter(s, now) {
			continue
		}

		if nearest == nil || s.StartScheduled.Before(*nearest) {
			t := *s.StartScheduled

			nearest = &t
		}
	}

	return nearest
}

func streamStartsAfter(stream *domain.Stream, now time.Time) bool {
	return stream.IsUpcoming() && stream.StartScheduled != nil && stream.StartScheduled.After(now)
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
