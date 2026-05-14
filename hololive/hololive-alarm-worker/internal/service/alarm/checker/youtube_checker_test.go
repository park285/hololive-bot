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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/notification"
)

type fakeYouTubeLiveSessionSource struct {
	sessions              []PersistedYouTubeLiveSession
	recentLiveChannelIDs  []string
	recentDispatch        map[string]bool
	recentSentRooms       map[string][]string
	recentSentRoomsErr    error
	loadRecentChannelArgs [][]string
}

func (s *fakeYouTubeLiveSessionSource) LoadRecentSessions(
	_ context.Context,
	ids []string,
	_ time.Time,
) ([]PersistedYouTubeLiveSession, error) {
	s.loadRecentChannelArgs = append(s.loadRecentChannelArgs, append([]string(nil), ids...))
	return s.sessions, nil
}

func (s *fakeYouTubeLiveSessionSource) LoadRecentLiveChannelIDs(
	_ context.Context,
	_ []string,
	_ time.Time,
) ([]string, error) {
	return s.recentLiveChannelIDs, nil
}

func (s *fakeYouTubeLiveSessionSource) RecentlyDispatchedStreamIDs(
	_ context.Context,
	streamIDs []string,
	_ time.Time,
) (map[string]struct{}, error) {
	dispatched := make(map[string]struct{})
	for _, streamID := range streamIDs {
		if s.recentDispatch[streamID] {
			dispatched[streamID] = struct{}{}
		}
	}
	return dispatched, nil
}

func (s *fakeYouTubeLiveSessionSource) RecentlySentLiveStreamRooms(
	_ context.Context,
	streamIDs []string,
	_ time.Time,
) (map[string]map[string]struct{}, error) {
	if s.recentSentRoomsErr != nil {
		return nil, s.recentSentRoomsErr
	}
	sentRoomsByStreamID := make(map[string]map[string]struct{})
	for _, streamID := range streamIDs {
		for _, roomID := range s.recentSentRooms[streamID] {
			if sentRoomsByStreamID[streamID] == nil {
				sentRoomsByStreamID[streamID] = make(map[string]struct{})
			}
			sentRoomsByStreamID[streamID][roomID] = struct{}{}
		}
	}
	return sentRoomsByStreamID, nil
}

func TestNewYouTubeChecker_NilDependencies(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, _ := holodex.NewHolodexService("http://unused", "k", cacheSvc, nil, logger)

	tests := map[string]struct {
		cacheNil   bool
		holodexNil bool
		tierNil    bool
		dedupNil   bool
		wantErr    string
	}{
		"cache nil":   {cacheNil: true, wantErr: "cache service is nil"},
		"holodex nil": {holodexNil: true, wantErr: "holodex service is nil"},
		"tier nil":    {tierNil: true, wantErr: "tier scheduler is nil"},
		"dedup nil":   {dedupNil: true, wantErr: "dedup service is nil"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := cacheSvc
			h := holodexSvc
			ts := tierSched
			d := dedupSvc

			if tc.cacheNil {
				c = nil
			}

			if tc.holodexNil {
				h = nil
			}

			if tc.tierNil {
				ts = nil
			}

			if tc.dedupNil {
				d = nil
			}

			_, err := NewYouTubeChecker(c, h, ts, d, []int{5}, 0, logger)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestYouTubeCheckerCheck_EmptyChannelRegistry(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService("http://unused", "k", cacheSvc, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, logger)
	require.NoError(t, err)

	notifications, checkErr := checker.Check(t.Context())
	require.NoError(t, checkErr)
	assert.Empty(t, notifications)
}

func TestYouTubeCheckerCheck_TableDrivenFiveCases(t *testing.T) {
	t.Parallel()

	const (
		channelID = "UC_TEST_CHANNEL"
		roomID    = "room-1"
		streamID  = "stream-test-1"
	)

	type testcase struct {
		name            string
		scenario        string
		ctxTimeout      time.Duration
		preMarkDedup    bool
		wantCount       int
		wantErrContains []string
	}

	buildUpcomingResponse := func(startScheduled time.Time) string {
		return fmt.Sprintf(
			`[{"id":"%s","title":"테스트 방송","channel_id":"%s","status":"upcoming","start_scheduled":"%s","channel":{"id":"%s","name":"테스트 채널","org":"Hololive"}}]`,
			streamID,
			channelID,
			startScheduled.UTC().Format(time.RFC3339),
			channelID,
		)
	}

	tests := []testcase{
		{
			name:      "정상",
			scenario:  "ok",
			wantCount: 1,
		},
		{
			name:      "미발견",
			scenario:  "not_found",
			wantCount: 0,
		},
		{
			name:            "timeout",
			scenario:        "timeout",
			ctxTimeout:      100 * time.Millisecond,
			wantErrContains: []string{"check youtube streams: fetch channels live status", "context deadline"},
		},
		{
			name:            "5xx",
			scenario:        "5xx",
			wantErrContains: []string{"Server error: 500"},
		},
		{
			name:         "dedup",
			scenario:     "dedup",
			preMarkDedup: true,
			wantCount:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			startScheduled := time.Now().UTC().Truncate(time.Second).Add(5*time.Minute + 10*time.Second)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/users/live" {
					http.NotFound(w, r)
					return
				}

				if !strings.Contains(r.URL.Query().Get("channels"), channelID) {
					http.Error(w, "missing channels query", http.StatusBadRequest)
					return
				}

				switch tc.scenario {
				case "ok", "dedup":
					w.Header().Set("Content-Type", "application/json")

					_, _ = w.Write([]byte(buildUpcomingResponse(startScheduled)))
				case "not_found":
					w.Header().Set("Content-Type", "application/json")

					_, _ = w.Write([]byte(`[]`))
				case "timeout":
					time.Sleep(150 * time.Millisecond)
					w.Header().Set("Content-Type", "application/json")

					_, _ = w.Write([]byte(`[]`))
				case "5xx":
					w.WriteHeader(http.StatusInternalServerError)

					_, _ = w.Write([]byte(`{"error":"server exploded"}`))
				default:
					http.Error(w, "unknown scenario", http.StatusInternalServerError)
				}
			}))
			defer server.Close()

			cacheSvc := newCheckerTestCacheClient(t)
			logger := newCheckerTestLogger()
			dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
			tierSched := tier.NewTieredScheduler(logger)
			holodexSvc, err := holodex.NewHolodexService(server.URL, "test-key", cacheSvc, nil, logger)
			require.NoError(t, err)

			checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, logger)
			require.NoError(t, err)

			setupCtx := t.Context()

			_, err = cacheSvc.SAdd(setupCtx, notification.AlarmChannelRegistryKey, []string{channelID})
			require.NoError(t, err)

			_, err = cacheSvc.SAdd(setupCtx, notification.ChannelSubscribersKeyPrefix+channelID, []string{roomID})
			require.NoError(t, err)

			if tc.preMarkDedup {
				err = dedupSvc.MarkAsNotified(setupCtx, streamID, startScheduled, 5)
				require.NoError(t, err)
			}

			runCtx := setupCtx

			if tc.ctxTimeout > 0 {
				var cancel context.CancelFunc

				runCtx, cancel = context.WithTimeout(setupCtx, tc.ctxTimeout)
				defer cancel()
			}

			notifications, checkErr := checker.Check(runCtx)

			if len(tc.wantErrContains) > 0 {
				require.Error(t, checkErr)
				for _, wantErrContains := range tc.wantErrContains {
					assert.Contains(t, checkErr.Error(), wantErrContains)
				}

				return
			}

			require.NoError(t, checkErr)
			assert.Len(t, notifications, tc.wantCount)
		})
	}
}

func TestYouTubeCheckerCheck_RecoversMissedPrimaryReminderFromLiveCatchup(t *testing.T) {
	t.Parallel()

	const (
		channelID = "UC_TEST_CHANNEL"
		roomID    = "room-1"
		streamID  = "stream-live-after-start"
	)

	startActual := time.Now().UTC().Truncate(time.Second).Add(-5 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/live" {
			http.NotFound(w, r)
			return
		}
		if !strings.Contains(r.URL.Query().Get("channels"), channelID) {
			http.Error(w, "missing channels query", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fmt.Appendf(nil,
			`[{"id":"%s","title":"테스트 방송","channel_id":"%s","status":"live","start_scheduled":"%s","start_actual":"%s","channel":{"id":"%s","name":"테스트 채널","org":"Hololive"}}]`,
			streamID,
			channelID,
			startActual.UTC().Format(time.RFC3339),
			startActual.UTC().Format(time.RFC3339),
			channelID,
		))
	}))
	defer server.Close()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService(server.URL, "test-key", cacheSvc, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, logger)
	require.NoError(t, err)

	ctx := t.Context()
	_, err = cacheSvc.SAdd(ctx, notification.AlarmChannelRegistryKey, []string{channelID})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+channelID, []string{roomID})
	require.NoError(t, err)

	notifications, err := checker.Check(ctx)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, 5, notifications[0].MinutesUntil)
}

func TestYouTubeCheckerCheck_UsesPersistedLiveSessionWhenHolodexOmitsLive(t *testing.T) {
	t.Parallel()

	const (
		channelID = "UC_TEST_CHANNEL"
		roomID    = "room-1"
		streamID  = "stream-live-from-db"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/live" {
			http.NotFound(w, r)
			return
		}
		if !strings.Contains(r.URL.Query().Get("channels"), channelID) {
			http.Error(w, "missing channels query", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService(server.URL, "test-key", cacheSvc, nil, logger)
	require.NoError(t, err)

	startActual := time.Now().UTC().Truncate(time.Second).Add(-30 * time.Minute)
	lastSeenAt := time.Now().UTC().Truncate(time.Second)
	persistedSource := &fakeYouTubeLiveSessionSource{
		sessions: []PersistedYouTubeLiveSession{{
			Stream: &domain.Stream{
				ID:          streamID,
				Title:       "DB live",
				ChannelID:   channelID,
				Status:      domain.StreamStatusLive,
				StartActual: &startActual,
				Channel:     &domain.Channel{ID: channelID, Name: "DB Channel"},
			},
			LastSeenAt: lastSeenAt,
		}},
		recentDispatch: map[string]bool{streamID: true},
	}

	checker, err := NewYouTubeCheckerWithPersistedLiveSource(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, persistedSource, logger)
	require.NoError(t, err)

	ctx := t.Context()
	_, err = cacheSvc.SAdd(ctx, notification.AlarmChannelRegistryKey, []string{channelID})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+channelID, []string{roomID})
	require.NoError(t, err)

	notifications, err := checker.Check(ctx)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, roomID, notifications[0].RoomID)
	assert.Equal(t, streamID, notifications[0].Stream.ID)
	assert.Equal(t, 5, notifications[0].MinutesUntil)
}

func TestYouTubeCheckerCheck_ForcesPersistedLiveChannelDueEvenWhenTierNotDue(t *testing.T) {
	t.Parallel()

	const (
		channelID = "UC_TEST_CHANNEL"
		roomID    = "room-1"
		streamID  = "stream-force-due"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/live" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService(server.URL, "test-key", cacheSvc, nil, logger)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	liveStart := now.Add(-2 * time.Minute)
	persistedSource := &fakeYouTubeLiveSessionSource{}

	checker, err := NewYouTubeCheckerWithPersistedLiveSource(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, persistedSource, logger)
	require.NoError(t, err)

	ctx := t.Context()
	_, err = cacheSvc.SAdd(ctx, notification.AlarmChannelRegistryKey, []string{channelID})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+channelID, []string{roomID})
	require.NoError(t, err)

	first, err := checker.Check(ctx)
	require.NoError(t, err)
	assert.Empty(t, first)

	persistedSource.recentLiveChannelIDs = []string{channelID}
	persistedSource.sessions = []PersistedYouTubeLiveSession{{
		Stream: &domain.Stream{
			ID:             streamID,
			Title:          "Persisted live",
			ChannelID:      channelID,
			Status:         domain.StreamStatusLive,
			StartScheduled: &liveStart,
			StartActual:    &liveStart,
			Channel:        &domain.Channel{ID: channelID, Name: "Persisted Channel"},
		},
		LastSeenAt: now,
	}}
	persistedSource.recentDispatch = map[string]bool{streamID: true}

	second, err := checker.Check(ctx)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, streamID, second[0].Stream.ID)
	require.NotEmpty(t, persistedSource.loadRecentChannelArgs)
	assert.Contains(t, persistedSource.loadRecentChannelArgs[len(persistedSource.loadRecentChannelArgs)-1], channelID)
}

func TestYouTubeCheckerCheck_UsesPersistedLiveSessionWhenHolodexFails(t *testing.T) {
	t.Parallel()

	const (
		channelID = "UC_TEST_CHANNEL"
		roomID    = "room-1"
		streamID  = "stream-live-holodex-failed"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/live" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "holodex unavailable", http.StatusInternalServerError)
	}))
	defer server.Close()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService(server.URL, "test-key", cacheSvc, nil, logger)
	require.NoError(t, err)

	startActual := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Minute)
	lastSeenAt := time.Now().UTC().Truncate(time.Second)
	persistedSource := &fakeYouTubeLiveSessionSource{
		sessions: []PersistedYouTubeLiveSession{{
			Stream: &domain.Stream{
				ID:          streamID,
				Title:       "DB live",
				ChannelID:   channelID,
				Status:      domain.StreamStatusLive,
				StartActual: &startActual,
				Channel:     &domain.Channel{ID: channelID, Name: "DB Channel"},
			},
			LastSeenAt: lastSeenAt,
		}},
		recentDispatch: map[string]bool{streamID: true},
	}

	checker, err := NewYouTubeCheckerWithPersistedLiveSource(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, persistedSource, logger)
	require.NoError(t, err)

	ctx := t.Context()
	_, err = cacheSvc.SAdd(ctx, notification.AlarmChannelRegistryKey, []string{channelID})
	require.NoError(t, err)
	_, err = cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+channelID, []string{roomID})
	require.NoError(t, err)

	notifications, err := checker.Check(ctx)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, streamID, notifications[0].Stream.ID)
}

func TestYouTubeChecker_RecoversRecentCappedFiveMinuteAlarm(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService("http://unused", "k", cacheSvc, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 75*time.Second, logger)
	require.NoError(t, err)

	now := time.Date(2026, 4, 9, 11, 56, 0, 0, time.UTC)
	startScheduled := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	window := sharedchecker.ResolveEvaluationWindow(
		time.Date(2026, 4, 9, 11, 51, 0, 0, time.UTC),
		now,
		75*time.Second,
	)

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "late-stream",
		ChannelID:      "ch-1",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &startScheduled,
		Channel:        &domain.Channel{ID: "ch-1", Name: "Channel 1"},
	}, []string{"room-1"}, window)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, 5, notifications[0].MinutesUntil)
}

func TestYouTubeChecker_BuildUpcomingNotifications_FallsBackToThreeMinuteTarget(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService("http://unused", "k", cacheSvc, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, logger)
	require.NoError(t, err)

	window := sharedchecker.EvaluationWindow{
		Start: time.Date(2026, 4, 9, 11, 56, 30, 0, time.UTC),
		End:   time.Date(2026, 4, 9, 11, 57, 10, 0, time.UTC),
	}
	startScheduled := time.Date(2026, 4, 9, 12, 0, 20, 0, time.UTC)

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "late-stream",
		ChannelID:      "ch-1",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &startScheduled,
		Channel:        &domain.Channel{ID: "ch-1", Name: "Channel 1"},
	}, []string{"room-1"}, window)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, 3, notifications[0].MinutesUntil)
}

func TestYouTubeChecker_BuildUpcomingNotifications_PrefersRecoveredFiveMinuteTargetOverCurrentThree(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService("http://unused", "k", cacheSvc, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, logger)
	require.NoError(t, err)

	window := sharedchecker.EvaluationWindow{
		Start:  time.Date(2026, 4, 9, 11, 55, 55, 0, time.UTC),
		End:    time.Date(2026, 4, 9, 11, 57, 10, 0, time.UTC),
		Capped: true,
	}
	startScheduled := time.Date(2026, 4, 9, 12, 1, 9, 0, time.UTC)

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "recovered-five-over-three",
		ChannelID:      "ch-1",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &startScheduled,
		Channel:        &domain.Channel{ID: "ch-1", Name: "Channel 1"},
	}, []string{"room-1"}, window)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, 5, notifications[0].MinutesUntil)
}

func TestYouTubeChecker_BuildUpcomingNotifications_DoesNotInventThreeMinuteTarget(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService("http://unused", "k", cacheSvc, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 1}, 0, logger)
	require.NoError(t, err)

	window := sharedchecker.EvaluationWindow{
		Start: time.Date(2026, 4, 9, 11, 56, 30, 0, time.UTC),
		End:   time.Date(2026, 4, 9, 11, 57, 10, 0, time.UTC),
	}
	startScheduled := time.Date(2026, 4, 9, 12, 0, 20, 0, time.UTC)

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "late-stream",
		ChannelID:      "ch-1",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &startScheduled,
		Channel:        &domain.Channel{ID: "ch-1", Name: "Channel 1"},
	}, []string{"room-1"}, window)
	require.NoError(t, err)
	assert.Empty(t, notifications)
}

func TestYouTubeChecker_BuildUpcomingNotifications_SendsScheduleDelayOnNonTargetMinute(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService("http://unused", "k", cacheSvc, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, logger)
	require.NoError(t, err)

	previousScheduled := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	currentScheduled := time.Date(2026, 4, 9, 12, 2, 0, 0, time.UTC)
	require.NoError(t, dedupSvc.MarkAsNotified(t.Context(), "delayed-stream", previousScheduled, 5))

	window := sharedchecker.EvaluationWindow{
		Start: time.Date(2026, 4, 9, 11, 52, 50, 0, time.UTC),
		End:   time.Date(2026, 4, 9, 11, 53, 10, 0, time.UTC),
	}

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "delayed-stream",
		ChannelID:      "ch-1",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &currentScheduled,
		Channel:        &domain.Channel{ID: "ch-1", Name: "Channel 1"},
	}, []string{"room-1"}, window)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, 8, notifications[0].MinutesUntil)
	assert.Equal(t, "일정이 늦춰졌습니다.", notifications[0].ScheduleChangeMessage)
}

func TestYouTubeChecker_BuildUpcomingNotifications_TargetReminderDoesNotCarryScheduleChange(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService("http://unused", "k", cacheSvc, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, logger)
	require.NoError(t, err)

	previousScheduled := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	currentScheduled := time.Date(2026, 4, 9, 12, 30, 0, 0, time.UTC)
	require.NoError(t, dedupSvc.MarkAsNotified(t.Context(), "delayed-target-stream", previousScheduled, 5))

	window := sharedchecker.EvaluationWindow{
		Start: time.Date(2026, 4, 9, 12, 24, 0, 0, time.UTC),
		End:   time.Date(2026, 4, 9, 12, 25, 0, 0, time.UTC),
	}

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "delayed-target-stream",
		ChannelID:      "ch-1",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &currentScheduled,
		Channel:        &domain.Channel{ID: "ch-1", Name: "Channel 1"},
	}, []string{"room-1"}, window)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, 5, notifications[0].MinutesUntil)
	assert.Empty(t, notifications[0].ScheduleChangeMessage)
	assert.Empty(t, notifications[0].ScheduleChangePreviousStart)
}

func TestYouTubeChecker_BuildUpcomingNotifications_DetectsReplacedWaitingRoomScheduleDelay(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	dedupSvc := dedup.NewService(cacheSvc, []int{5, 3, 1}, logger)
	tierSched := tier.NewTieredScheduler(logger)
	holodexSvc, err := holodex.NewHolodexService("http://unused", "k", cacheSvc, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeChecker(cacheSvc, holodexSvc, tierSched, dedupSvc, []int{5, 3, 1}, 0, logger)
	require.NoError(t, err)

	previousScheduled := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	currentScheduled := time.Date(2026, 4, 9, 12, 2, 0, 0, time.UTC)
	previousStream := &domain.Stream{
		ID:             "old-waiting-room",
		Title:          "same broadcast title",
		ChannelID:      "ch-1",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &previousScheduled,
		Channel:        &domain.Channel{ID: "ch-1", Name: "Channel 1"},
	}
	require.NoError(t, dedupSvc.MarkUpcomingEventNotified(t.Context(), "room-1", "ch-1", previousStream))

	window := sharedchecker.EvaluationWindow{
		Start: time.Date(2026, 4, 9, 11, 52, 50, 0, time.UTC),
		End:   time.Date(2026, 4, 9, 11, 53, 10, 0, time.UTC),
	}

	notifications, err := checker.buildUpcomingNotifications(t.Context(), &domain.Stream{
		ID:             "new-waiting-room",
		Title:          "same broadcast title",
		ChannelID:      "ch-1",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: &currentScheduled,
		Channel:        &domain.Channel{ID: "ch-1", Name: "Channel 1"},
	}, []string{"room-1", "room-2"}, window)
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	assert.Equal(t, "room-1", notifications[0].RoomID)
	assert.Equal(t, 8, notifications[0].MinutesUntil)
	assert.Equal(t, "일정이 늦춰졌습니다.", notifications[0].ScheduleChangeMessage)
}

func TestUniqueStrings(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input []string
		want  []string
	}{
		"nil": {input: nil, want: nil},
		"단일":  {input: []string{"a"}, want: []string{"a"}},
		"중복 제거": {
			input: []string{"a", "b", "a", "c", "b"},
			want:  []string{"a", "b", "c"},
		},
		"빈 문자열 필터링": {
			input: []string{"a", "", "b", ""},
			want:  []string{"a", "b"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, uniqueStrings(tc.input))
		})
	}
}

func TestNormalizeTargetMinutes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input []int
		want  []int
	}{
		"nil 기본값":       {input: nil, want: []int{5, 3, 1}},
		"빈 슬라이스 기본값":    {input: []int{}, want: []int{5, 3, 1}},
		"음수만 있으면 기본값":   {input: []int{-1, -5, 0}, want: []int{5, 3, 1}},
		"내림차순 정렬 + 1추가": {input: []int{3, 10, 5}, want: []int{10, 5, 3, 1}},
		"이미 1 포함":       {input: []int{5, 1, 3}, want: []int{5, 3, 1}},
		"중복 제거":         {input: []int{5, 5, 3, 3, 1}, want: []int{5, 3, 1}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, sharedchecker.NormalizeTargetMinutes(tc.input))
		})
	}
}

func TestCloneStream(t *testing.T) {
	t.Parallel()

	t.Run("nil 입력", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, cloneStream(nil))
	})

	t.Run("deep copy 검증", func(t *testing.T) {
		t.Parallel()

		start := time.Now().UTC()
		actual := start.Add(-5 * time.Minute)
		original := &domain.Stream{
			ID:             "s1",
			StartScheduled: &start,
			StartActual:    &actual,
			Channel:        &domain.Channel{ID: "ch1"},
		}
		copied := cloneStream(original)

		assert.Equal(t, original.ID, copied.ID)
		assert.NotSame(t, original.StartScheduled, copied.StartScheduled)
		assert.NotSame(t, original.StartActual, copied.StartActual)
		assert.NotSame(t, original.Channel, copied.Channel)
	})
}

func TestEnsureScheduledTime(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	scheduled := now.Add(10 * time.Minute)
	actual := now.Add(-5 * time.Minute)

	tests := map[string]struct {
		stream       *domain.Stream
		fallback     time.Time
		wantNil      bool
		wantHasSched bool
	}{
		"nil 스트림":           {stream: nil, wantNil: true},
		"이미 StartScheduled": {stream: &domain.Stream{StartScheduled: &scheduled}, wantHasSched: true},
		"StartActual 폴백":    {stream: &domain.Stream{StartActual: &actual}, wantHasSched: true},
		"fallback 사용":       {stream: &domain.Stream{}, fallback: now, wantHasSched: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := ensureScheduledTime(tc.stream, tc.fallback)
			if tc.wantNil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)

				if tc.wantHasSched {
					assert.NotNil(t, got.StartScheduled)
				}
			}
		})
	}
}

func TestRoomNotifications(t *testing.T) {
	t.Parallel()

	ch := &domain.Channel{ID: "ch1"}
	s := &domain.Stream{ID: "s1"}

	tests := map[string]struct {
		roomIDs []string
		stream  *domain.Stream
		wantLen int
	}{
		"빈 방":        {roomIDs: nil, stream: s, wantLen: 0},
		"nil 스트림":    {roomIDs: []string{"r1"}, stream: nil, wantLen: 0},
		"정상":         {roomIDs: []string{"r1", "r2"}, stream: s, wantLen: 2},
		"빈 방 ID 필터링": {roomIDs: []string{"r1", "", "r2"}, stream: s, wantLen: 2},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Len(t, roomNotifications(tc.roomIDs, ch, tc.stream, 5, ""), tc.wantLen)
		})
	}
}

func TestLoadSubscriberRoomsByChannel_Table(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	ctx := t.Context()

	_, err := cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"ch1", []string{"r1", "r2"})
	require.NoError(t, err)

	_, err = cacheSvc.SAdd(ctx, notification.ChannelSubscribersKeyPrefix+"ch2", []string{"r3"})
	require.NoError(t, err)

	tests := map[string]struct {
		channelIDs []string
		wantLen    int
	}{
		"빈 채널":     {channelIDs: nil, wantLen: 0},
		"구독자 있음":   {channelIDs: []string{"ch1", "ch2"}, wantLen: 2},
		"구독자 없음":   {channelIDs: []string{"unknown"}, wantLen: 0},
		"중복 채널 제거": {channelIDs: []string{"ch1", "ch1"}, wantLen: 1},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, loadErr := loadSubscriberRoomsByChannel(ctx, cacheSvc, tc.channelIDs)
			require.NoError(t, loadErr)
			assert.Len(t, got, tc.wantLen)
		})
	}
}

func TestSafeLogger(t *testing.T) {
	t.Parallel()

	t.Run("nil 반환 기본 로거", func(t *testing.T) {
		t.Parallel()
		assert.NotNil(t, safeLogger(nil))
	})

	t.Run("정상 로거 통과", func(t *testing.T) {
		t.Parallel()

		l := newCheckerTestLogger()
		assert.Same(t, l, safeLogger(l))
	})
}

func TestGroupStreamsByChannel(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		streams []*domain.Stream
		wantLen int
	}{
		"nil 스트림 무시":     {streams: []*domain.Stream{nil}, wantLen: 0},
		"빈 channelID 무시": {streams: []*domain.Stream{{ID: "s1"}}, wantLen: 0},
		"정상 그룹핑": {
			streams: []*domain.Stream{
				{ID: "s1", ChannelID: "ch1"},
				{ID: "s2", ChannelID: "ch1"},
				{ID: "s3", ChannelID: "ch2"},
			},
			wantLen: 2,
		},
		"Channel 객체 폴백": {
			streams: []*domain.Stream{{ID: "s1", Channel: &domain.Channel{ID: "ch1"}}},
			wantLen: 1,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Len(t, groupStreamsByChannel(tc.streams), tc.wantLen)
		})
	}
}

func TestResolveLiveStart(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	earlier := now.Add(-10 * time.Minute)

	tests := map[string]struct {
		stream  *domain.Stream
		wantNil bool
	}{
		"nil 스트림":           {stream: nil, wantNil: true},
		"StartActual 우선":    {stream: &domain.Stream{StartActual: &now, StartScheduled: &earlier}, wantNil: false},
		"StartScheduled 폴백": {stream: &domain.Stream{StartScheduled: &now}, wantNil: false},
		"양쪽 nil":            {stream: &domain.Stream{}, wantNil: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := resolveLiveStart(tc.stream)
			if tc.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
			}
		})
	}
}

func TestIsChzzkLive(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status *chzzk.LiveStatusContent
		want   bool
	}{
		"nil":      {status: nil, want: false},
		"OPEN":     {status: &chzzk.LiveStatusContent{Status: "OPEN"}, want: true},
		"open 소문자": {status: &chzzk.LiveStatusContent{Status: "open"}, want: true},
		"CLOSE":    {status: &chzzk.LiveStatusContent{Status: "CLOSE"}, want: false},
		"공백 포함":    {status: &chzzk.LiveStatusContent{Status: "  OPEN  "}, want: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isChzzkLive(tc.status))
		})
	}
}

func TestBuildChzzkLiveDedupKey(t *testing.T) {
	t.Parallel()

	detectedAt := time.Date(2026, time.March, 2, 10, 35, 0, 0, time.UTC)
	key := buildChzzkLiveDedupKey("chzzk123", detectedAt)

	assert.Contains(t, key, notification.ChzzkLiveNotifiedKeyPrefix+"chzzk123:")
	assert.Contains(t, key, "20260302T1030")
}

func TestBuildChzzkLiveStream(t *testing.T) {
	t.Parallel()

	status := &chzzk.LiveStatusContent{
		LiveTitle:           "테스트 라이브",
		Status:              "OPEN",
		ConcurrentUserCount: 1234,
		LiveCategoryValue:   "게임",
	}
	detectedAt := time.Date(2026, time.March, 2, 10, 35, 0, 0, time.UTC)

	stream := buildChzzkLiveStream("UC_YT", "chzzk123", status, detectedAt)
	require.NotNil(t, stream)
	assert.Equal(t, domain.StreamStatusLive, stream.Status)
	assert.Equal(t, "테스트 라이브", stream.Title)
	assert.Equal(t, "UC_YT", stream.ChannelID)
	assert.True(t, stream.IsChzzkOnly)
	assert.Contains(t, stream.ChzzkLiveURL, "chzzk123")
	require.NotNil(t, stream.ViewerCount)
	assert.Equal(t, 1234, *stream.ViewerCount)
}

func TestBuildChzzkLiveStream_EmptyTitle(t *testing.T) {
	t.Parallel()

	status := &chzzk.LiveStatusContent{Status: "OPEN"}
	now := time.Now().UTC()
	stream := buildChzzkLiveStream("UC_YT", "ch1", status, now)

	assert.Contains(t, stream.Title, "치지직 라이브")
}

func TestBuildTwitchLiveDedupKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, twitchLiveNotifiedKeyPrefix+"u1:s1", buildTwitchLiveDedupKey("u1", "s1"))
}

func TestNormalizeTwitchLoginMappings(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input   map[string]string
		wantLen int
	}{
		"nil":     {input: nil, wantLen: 0},
		"정상":      {input: map[string]string{"LOGIN": "UC_A"}, wantLen: 1},
		"빈 값 필터링": {input: map[string]string{"login": "", "": "UC_B"}, wantLen: 0},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m, ids := normalizeTwitchLoginMappings(tc.input)
			assert.Len(t, m, tc.wantLen)
			assert.Len(t, ids, tc.wantLen)
		})
	}
}

func TestBuildTwitchLookupLogins(t *testing.T) {
	t.Parallel()

	mappings := map[string]string{"a": "UC_A", "b": "UC_B", "c": "UC_C"}
	subs := map[string][]string{"UC_A": {"r1"}, "UC_C": {"r2"}}

	logins := buildTwitchLookupLogins(mappings, subs)
	assert.Len(t, logins, 2)
	assert.Contains(t, logins, "a")
	assert.Contains(t, logins, "c")
}
