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

package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/checker"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

type runnerFunc struct {
	check func(context.Context) ([]*domain.AlarmNotification, error)
}

func (r *runnerFunc) Check(ctx context.Context) ([]*domain.AlarmNotification, error) {
	if r.check == nil {
		return nil, nil
	}

	return r.check(ctx)
}

type senderFunc struct {
	send func(context.Context, []*domain.AlarmNotification) (checker.SendResult, error)
}

func (s *senderFunc) Send(ctx context.Context, notifications []*domain.AlarmNotification) (checker.SendResult, error) {
	if s.send == nil {
		return checker.SendResult{}, nil
	}

	return s.send(ctx, notifications)
}

type targetMinutesSourceStub struct {
	targets []int
}

func (s *targetMinutesSourceStub) GetTargetMinutes() []int {
	return append([]int(nil), s.targets...)
}

type targetMinutesUpdaterStub struct {
	calls [][]int
}

func (s *targetMinutesUpdaterStub) UpdateTargetMinutes(targets []int) {
	s.calls = append(s.calls, append([]int(nil), targets...))
}

func testSchedulerLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNormalizeTargetMinutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []int
		expected []int
	}{
		{name: "empty uses defaults", input: nil, expected: []int{5, 3, 1}},
		{name: "filters and deduplicates", input: []int{10, 0, 10, -1, 3}, expected: []int{10, 3, 1}},
		{name: "keeps one fallback", input: []int{15, 1, 5}, expected: []int{15, 5, 1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, normalizeTargetMinutes(tc.input))
		})
	}
}

func TestRuntimeSchedulerStart_NilContext(t *testing.T) {
	t.Parallel()

	s := &RuntimeScheduler{
		logger: testSchedulerLogger(),
	}

	s.Start(nil)
}

func TestRuntimeSchedulerRunIterations(t *testing.T) {
	t.Parallel()

	notify := []*domain.AlarmNotification{
		{RoomID: "room-1"},
	}

	t.Run("youtube check failure", func(t *testing.T) {
		s := &RuntimeScheduler{
			youtubeChecker: &runnerFunc{
				check: func(context.Context) ([]*domain.AlarmNotification, error) {
					return nil, errors.New("youtube check failed")
				},
			},
			notifier: &senderFunc{},
			logger:   testSchedulerLogger(),
		}

		err := s.runYouTubeIteration(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "run youtube iteration: check notifications")
	})

	t.Run("youtube dispatch failure", func(t *testing.T) {
		s := &RuntimeScheduler{
			youtubeChecker: &runnerFunc{
				check: func(context.Context) ([]*domain.AlarmNotification, error) {
					return notify, nil
				},
			},
			notifier: &senderFunc{
				send: func(context.Context, []*domain.AlarmNotification) (checker.SendResult, error) {
					return checker.SendResult{}, errors.New("send failed")
				},
			},
			logger: testSchedulerLogger(),
		}

		err := s.runYouTubeIteration(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dispatch notifications: send notifications")
	})

	t.Run("youtube iteration syncs target minutes before check", func(t *testing.T) {
		youtubeUpdater := &targetMinutesUpdaterStub{}
		dedupUpdater := &targetMinutesUpdaterStub{}
		s := &RuntimeScheduler{
			youtubeChecker: &runnerFunc{
				check: func(context.Context) ([]*domain.AlarmNotification, error) {
					return nil, nil
				},
			},
			youtubeTargetUpdater: youtubeUpdater,
			dedupTargetUpdater:   dedupUpdater,
			targetMinutesSource:  &targetMinutesSourceStub{targets: []int{10, 3, 1}},
			notifier:             &senderFunc{},
			logger:               testSchedulerLogger(),
		}

		require.NoError(t, s.runYouTubeIteration(t.Context()))
		assert.Equal(t, [][]int{{10, 3, 1}}, youtubeUpdater.calls)
		assert.Equal(t, [][]int{{10, 3, 1}}, dedupUpdater.calls)
	})

	t.Run("youtube iteration picks up updated alarm service targets", func(t *testing.T) {
		alarmSvc, err := notification.NewAlarmService(nil, nil, nil, nil, nil, nil, testSchedulerLogger(), []int{5, 3, 1})
		require.NoError(t, err)

		youtubeUpdater := &targetMinutesUpdaterStub{}
		dedupUpdater := &targetMinutesUpdaterStub{}
		s := &RuntimeScheduler{
			youtubeChecker: &runnerFunc{
				check: func(context.Context) ([]*domain.AlarmNotification, error) {
					return nil, nil
				},
			},
			youtubeTargetUpdater: youtubeUpdater,
			dedupTargetUpdater:   dedupUpdater,
			targetMinutesSource:  alarmSvc,
			notifier:             &senderFunc{},
			logger:               testSchedulerLogger(),
		}

		updated := alarmSvc.UpdateAlarmAdvanceMinutes(t.Context(), 12)
		require.Equal(t, []int{12, 3, 1}, updated)

		require.NoError(t, s.runYouTubeIteration(t.Context()))
		assert.Equal(t, [][]int{{12, 3, 1}}, youtubeUpdater.calls)
		assert.Equal(t, [][]int{{12, 3, 1}}, dedupUpdater.calls)
	})

	t.Run("chzzk success", func(t *testing.T) {
		sent := atomic.Int32{}
		s := &RuntimeScheduler{
			chzzkChecker: &runnerFunc{
				check: func(context.Context) ([]*domain.AlarmNotification, error) {
					return notify, nil
				},
			},
			notifier: &senderFunc{
				send: func(context.Context, []*domain.AlarmNotification) (checker.SendResult, error) {
					sent.Add(1)
					return checker.SendResult{Sent: 1}, nil
				},
			},
			logger: testSchedulerLogger(),
		}

		require.NoError(t, s.runChzzkIteration(t.Context()))
		assert.Equal(t, int32(1), sent.Load())
	})

	t.Run("twitch success empty notifications short-circuit", func(t *testing.T) {
		sent := atomic.Int32{}
		s := &RuntimeScheduler{
			twitchChecker: &runnerFunc{
				check: func(context.Context) ([]*domain.AlarmNotification, error) {
					return nil, nil
				},
			},
			notifier: &senderFunc{
				send: func(context.Context, []*domain.AlarmNotification) (checker.SendResult, error) {
					sent.Add(1)
					return checker.SendResult{}, nil
				},
			},
			logger: testSchedulerLogger(),
		}

		require.NoError(t, s.runTwitchIteration(t.Context()))
		assert.Equal(t, int32(0), sent.Load())
	})
}

func TestRuntimeSchedulerRunLoop_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	s := &RuntimeScheduler{
		logger: testSchedulerLogger(),
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		s.runLoop(ctx, "test", 10*time.Millisecond, 50*time.Millisecond, func(context.Context) error {
			calls.Add(1)
			return errors.New("expected failure branch")
		})
	}()

	require.Eventually(t, func() bool { return calls.Load() >= 2 }, time.Second, 20*time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runLoop did not stop after context cancellation")
	}
}

func TestNextAligned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		now      time.Time
		interval time.Duration
		want     time.Time
	}{
		{
			name:     "minute interval snaps to next minute",
			now:      time.Date(2026, time.April, 9, 10, 0, 5, 0, time.UTC),
			interval: time.Minute,
			want:     time.Date(2026, time.April, 9, 10, 1, 0, 0, time.UTC),
		},
		{
			name:     "exact boundary advances one interval",
			now:      time.Date(2026, time.April, 9, 10, 1, 0, 0, time.UTC),
			interval: time.Minute,
			want:     time.Date(2026, time.April, 9, 10, 2, 0, 0, time.UTC),
		},
		{
			name:     "three minute interval aligns to shared boundary",
			now:      time.Date(2026, time.April, 9, 10, 1, 10, 0, time.UTC),
			interval: 3 * time.Minute,
			want:     time.Date(2026, time.April, 9, 10, 3, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := nextAligned(tc.now, tc.interval)
			assert.Equal(t, tc.want, got)
		})
	}
}
