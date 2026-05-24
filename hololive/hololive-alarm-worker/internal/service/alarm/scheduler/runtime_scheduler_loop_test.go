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
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedcache "github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker"
)

func TestNextLoopDelay(t *testing.T) {
	t.Parallel()

	t.Run("returns positive delay between intervals", func(t *testing.T) {
		t.Parallel()

		delay := nextLoopDelay(time.Now(), time.Hour)

		assert.Greater(t, delay, time.Duration(0))
		assert.LessOrEqual(t, delay, time.Hour)
	})

	t.Run("returns zero when aligned boundary is already past", func(t *testing.T) {
		t.Parallel()

		delay := nextLoopDelay(time.Now().Add(-2*time.Hour), time.Minute)

		assert.Equal(t, time.Duration(0), delay)
	})
}

func TestFirstAlignedRunAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		now      time.Time
		interval time.Duration
		want     time.Time
	}{
		{
			name:     "zero interval returns now",
			now:      time.Date(2026, time.May, 24, 10, 15, 30, 0, time.UTC),
			interval: 0,
			want:     time.Date(2026, time.May, 24, 10, 15, 30, 0, time.UTC),
		},
		{
			name:     "negative interval returns now",
			now:      time.Date(2026, time.May, 24, 10, 15, 30, 0, time.UTC),
			interval: -time.Minute,
			want:     time.Date(2026, time.May, 24, 10, 15, 30, 0, time.UTC),
		},
		{
			name:     "exact boundary returns now",
			now:      time.Date(2026, time.May, 24, 10, 15, 0, 0, time.UTC),
			interval: 5 * time.Minute,
			want:     time.Date(2026, time.May, 24, 10, 15, 0, 0, time.UTC),
		},
		{
			name:     "off boundary returns next aligned time",
			now:      time.Date(2026, time.May, 24, 10, 16, 30, 0, time.UTC),
			interval: 5 * time.Minute,
			want:     time.Date(2026, time.May, 24, 10, 20, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, firstAlignedRunAt(tc.now, tc.interval))
		})
	}
}

func TestRuntimeSchedulerDispatchNotifications(t *testing.T) {
	t.Parallel()

	t.Run("empty notifications return nil without sending", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32
		s := &RuntimeScheduler{
			notifier: &senderFunc{
				send: func(context.Context, []*domain.AlarmNotification) (checker.SendResult, error) {
					calls.Add(1)
					return checker.SendResult{}, nil
				},
			},
			logger: testSchedulerLogger(),
		}

		require.NoError(t, s.dispatchNotifications(t.Context(), "youtube", nil))
		assert.Equal(t, int32(0), calls.Load())
	})

	t.Run("successful send with notifications", func(t *testing.T) {
		t.Parallel()

		notifications := []*domain.AlarmNotification{{RoomID: "room-1"}}
		var calls atomic.Int32
		var gotNotifications []*domain.AlarmNotification
		s := &RuntimeScheduler{
			notifier: &senderFunc{
				send: func(_ context.Context, notifications []*domain.AlarmNotification) (checker.SendResult, error) {
					calls.Add(1)
					gotNotifications = notifications
					return checker.SendResult{Sent: 1}, nil
				},
			},
			logger: testSchedulerLogger(),
		}

		require.NoError(t, s.dispatchNotifications(t.Context(), runtimeSchedulerLoopNameTwitch, notifications))
		assert.Equal(t, int32(1), calls.Load())
		assert.Equal(t, notifications, gotNotifications)
	})

	t.Run("partial failure from sender", func(t *testing.T) {
		t.Parallel()

		notifications := []*domain.AlarmNotification{{RoomID: "room-1"}, {RoomID: "room-2"}}
		sendErr := errors.New("sender failed")
		var calls atomic.Int32
		s := &RuntimeScheduler{
			notifier: &senderFunc{
				send: func(context.Context, []*domain.AlarmNotification) (checker.SendResult, error) {
					calls.Add(1)
					return checker.SendResult{Sent: 1, Failed: 1}, sendErr
				},
			},
			logger: testSchedulerLogger(),
		}

		err := s.dispatchNotifications(t.Context(), "youtube", notifications)

		require.ErrorIs(t, err, sendErr)
		assert.Contains(t, err.Error(), "partially failed")
		assert.Equal(t, int32(1), calls.Load())
	})
}

func TestRuntimeSchedulerSyncYouTubeTargetMinutes(t *testing.T) {
	t.Parallel()

	t.Run("no op when target minutes source is nil", func(t *testing.T) {
		t.Parallel()

		youtubeUpdater := &targetMinutesUpdaterStub{}
		dedupUpdater := &targetMinutesUpdaterStub{}
		s := &RuntimeScheduler{
			youtubeTargetUpdater: youtubeUpdater,
			dedupTargetUpdater:   dedupUpdater,
		}

		s.syncYouTubeTargetMinutes()

		assert.Empty(t, youtubeUpdater.calls)
		assert.Empty(t, dedupUpdater.calls)
	})

	t.Run("updates youtube and dedup target updaters", func(t *testing.T) {
		t.Parallel()

		youtubeUpdater := &targetMinutesUpdaterStub{}
		dedupUpdater := &targetMinutesUpdaterStub{}
		s := &RuntimeScheduler{
			youtubeTargetUpdater: youtubeUpdater,
			dedupTargetUpdater:   dedupUpdater,
			targetMinutesSource:  &targetMinutesSourceStub{targets: []int{12, 5, 1}},
		}

		s.syncYouTubeTargetMinutes()

		assert.Equal(t, [][]int{{12, 5, 1}}, youtubeUpdater.calls)
		assert.Equal(t, [][]int{{12, 5, 1}}, dedupUpdater.calls)
	})

	t.Run("handles nil updaters gracefully", func(t *testing.T) {
		t.Parallel()

		s := &RuntimeScheduler{
			targetMinutesSource: &targetMinutesSourceStub{targets: []int{10, 3, 1}},
		}

		require.NotPanics(t, s.syncYouTubeTargetMinutes)
	})
}

func TestRuntimeSchedulerRunAlarmCacheRecoveryLoop(t *testing.T) {
	t.Parallel()

	t.Run("nil scheduler returns nil", func(t *testing.T) {
		t.Parallel()

		var s *RuntimeScheduler

		require.NoError(t, s.runAlarmCacheRecoveryLoop(t.Context()))
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		t.Parallel()

		s := &RuntimeScheduler{logger: testSchedulerLogger()}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() {
			done <- s.runAlarmCacheRecoveryLoop(ctx)
		}()
		cancel()

		select {
		case err := <-done:
			require.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("alarm cache recovery loop did not stop after context cancellation")
		}
	})
}

func TestRuntimeSchedulerRecoverAlarmCacheAfterCheckFailure(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for non cache errors", func(t *testing.T) {
		t.Parallel()

		s := &RuntimeScheduler{
			cacheClient: cachemocks.NewStrictClient(),
			logger:      testSchedulerLogger(),
		}

		require.NoError(t, s.recoverAlarmCacheAfterCheckFailure(t.Context(), errors.New("plain error")))
	})

	t.Run("returns nil when check error is nil", func(t *testing.T) {
		t.Parallel()

		s := &RuntimeScheduler{
			cacheClient: cachemocks.NewStrictClient(),
			logger:      testSchedulerLogger(),
		}

		require.NoError(t, s.recoverAlarmCacheAfterCheckFailure(t.Context(), nil))
	})

	t.Run("returns nil when cache client is nil", func(t *testing.T) {
		t.Parallel()

		s := &RuntimeScheduler{logger: testSchedulerLogger()}
		checkErr := sharedcache.NewCacheError("failed", "smembers", "alarm:test", errors.New("EOF"))

		require.NoError(t, s.recoverAlarmCacheAfterCheckFailure(t.Context(), checkErr))
	})
}

func TestIsCacheFailure(t *testing.T) {
	t.Parallel()

	t.Run("returns true for cache error", func(t *testing.T) {
		t.Parallel()

		err := sharedcache.NewCacheError("failed", "get", "alarm:test", errors.New("EOF"))

		assert.True(t, isCacheFailure(err))
	})

	t.Run("returns false for plain error", func(t *testing.T) {
		t.Parallel()

		assert.False(t, isCacheFailure(errors.New("plain error")))
	})

	t.Run("returns false for wrapped non cache error", func(t *testing.T) {
		t.Parallel()

		err := fmt.Errorf("wrapped: %w", errors.New("plain error"))

		assert.False(t, isCacheFailure(err))
	})
}

func TestRuntimeSchedulerPlatformMappingMissing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		existsByKey map[string]bool
		want        bool
		wantCalls   []string
	}{
		{
			name:        "returns false when key exists",
			existsByKey: map[string]bool{"platform:key": true},
			want:        false,
			wantCalls:   []string{"platform:key"},
		},
		{
			name:        "returns false when key missing but empty marker exists",
			existsByKey: map[string]bool{"platform:empty": true},
			want:        false,
			wantCalls:   []string{"platform:key", "platform:empty"},
		},
		{
			name:        "returns true when key and empty marker are missing",
			existsByKey: map[string]bool{},
			want:        true,
			wantCalls:   []string{"platform:key", "platform:empty"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var calls []string
			cache := cachemocks.NewStrictClient()
			cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
				calls = append(calls, key)
				exists, ok := tc.existsByKey[key]
				if ok {
					return exists, nil
				}

				return false, nil
			}
			s := &RuntimeScheduler{cacheClient: cache}

			got, err := s.platformMappingMissing(t.Context(), alarmPlatformMappingKeys{
				key:            "platform:key",
				emptyMarkerKey: "platform:empty",
			})

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantCalls, calls)
		})
	}
}
