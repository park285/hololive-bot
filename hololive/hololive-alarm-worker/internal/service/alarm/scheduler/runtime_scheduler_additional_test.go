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
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	sharedcache "github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/notification"
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

type alarmCacheWarmerStub struct {
	calls     atomic.Int32
	syncCalls atomic.Int32
	err       error
	syncErr   error
}

func (s *alarmCacheWarmerStub) WarmCacheFromDB(context.Context) error {
	s.calls.Add(1)
	return s.err
}

func (s *alarmCacheWarmerStub) SyncPlatformMappings(context.Context) error {
	s.syncCalls.Add(1)
	return s.syncErr
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
			assert.Equal(t, tc.expected, sharedchecker.NormalizeTargetMinutes(tc.input))
		})
	}
}

func TestYouTubeEvaluationWindowCap(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 75*time.Second, youtubeEvaluationWindowCap(0))
	assert.Equal(t, 75*time.Second, youtubeEvaluationWindowCap(30*time.Second))
	assert.Equal(t, 135*time.Second, youtubeEvaluationWindowCap(2*time.Minute))
}

func TestAlarmCacheRecoveryInterval(t *testing.T) {
	t.Parallel()

	assert.Equal(t, time.Minute, alarmCacheRecoveryInterval)
}

func TestRuntimeSchedulerStart_NilContext(t *testing.T) {
	t.Parallel()

	s := &RuntimeScheduler{
		logger: testSchedulerLogger(),
	}

	var nilCtx context.Context

	require.Error(t, s.Start(nilCtx))
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
		alarmService, err := notification.NewAlarmService(nil, nil, nil, nil, nil, nil, testSchedulerLogger(), []int{5, 3, 1})
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
			targetMinutesSource:  alarmService,
			notifier:             &senderFunc{},
			logger:               testSchedulerLogger(),
		}

		updated := alarmService.UpdateAlarmAdvanceMinutes(t.Context(), 12)
		require.Equal(t, []int{12, 3, 1}, updated)

		require.NoError(t, s.runYouTubeIteration(t.Context()))
		assert.Equal(t, [][]int{{12, 3, 1}}, youtubeUpdater.calls)
		assert.Equal(t, [][]int{{12, 3, 1}}, dedupUpdater.calls)
	})

	t.Run("youtube cache failure triggers immediate cache recovery", func(t *testing.T) {
		warmer := &alarmCacheWarmerStub{}
		waitUntilReadyCalls := atomic.Int32{}
		cache := cachemocks.NewStrictClient()
		cache.WaitUntilReadyFunc = func(context.Context, time.Duration) error {
			waitUntilReadyCalls.Add(1)
			return nil
		}
		cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
			switch key {
			case sharedalarmkeys.AlarmChannelRegistryKey,
				sharedalarmkeys.AlarmSubscriberCacheEmptyKey:
			default:
				t.Fatalf("unexpected key: %s", key)
			}
			return false, nil
		}

		s := &RuntimeScheduler{
			youtubeChecker: &runnerFunc{
				check: func(context.Context) ([]*domain.AlarmNotification, error) {
					return nil, sharedcache.NewCacheError("failed", "smembers", sharedalarmkeys.AlarmChannelRegistryKey, errors.New("EOF"))
				},
			},
			cacheClient:           cache,
			alarmCacheWarmer:      warmer,
			platformMappingSyncer: warmer,
			notifier:              &senderFunc{},
			logger:                testSchedulerLogger(),
		}

		err := s.runYouTubeIteration(t.Context())
		require.Error(t, err)
		assert.Equal(t, int32(1), waitUntilReadyCalls.Load())
		assert.Equal(t, int32(1), warmer.calls.Load())
		assert.Equal(t, int32(1), warmer.syncCalls.Load())
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

func TestRuntimeSchedulerRecoverAlarmCacheIfRegistryEmpty(t *testing.T) {
	t.Parallel()

	t.Run("rebuilds subscriber cache and platform mappings when channel registry key is missing", func(t *testing.T) {
		warmer := &alarmCacheWarmerStub{}
		cache := cachemocks.NewStrictClient()
		cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
			switch key {
			case sharedalarmkeys.AlarmChannelRegistryKey,
				sharedalarmkeys.AlarmSubscriberCacheEmptyKey:
			default:
				t.Fatalf("unexpected key: %s", key)
			}
			return false, nil
		}

		s := &RuntimeScheduler{
			cacheClient:           cache,
			alarmCacheWarmer:      warmer,
			platformMappingSyncer: warmer,
			logger:                testSchedulerLogger(),
		}

		require.NoError(t, s.recoverAlarmCacheIfRegistryEmpty(t.Context(), "test"))
		assert.Equal(t, int32(1), warmer.calls.Load())
		assert.Equal(t, int32(1), warmer.syncCalls.Load())
	})

	t.Run("does not rebuild when channel registry is missing because DB alarm state is empty", func(t *testing.T) {
		warmer := &alarmCacheWarmerStub{}
		cache := cachemocks.NewStrictClient()
		cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
			switch key {
			case sharedalarmkeys.AlarmChannelRegistryKey:
				return false, nil
			case sharedalarmkeys.AlarmSubscriberCacheEmptyKey:
				return true, nil
			default:
				t.Fatalf("unexpected key: %s", key)
				return false, nil
			}
		}

		s := &RuntimeScheduler{
			cacheClient:           cache,
			alarmCacheWarmer:      warmer,
			platformMappingSyncer: warmer,
			logger:                testSchedulerLogger(),
		}

		require.NoError(t, s.recoverAlarmCacheIfRegistryEmpty(t.Context(), "test"))
		assert.Equal(t, int32(0), warmer.calls.Load())
		assert.Equal(t, int32(0), warmer.syncCalls.Load())
	})

	t.Run("does not warm cache when registry key exists", func(t *testing.T) {
		warmer := &alarmCacheWarmerStub{}
		cache := cachemocks.NewStrictClient()
		cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
			switch key {
			case sharedalarmkeys.AlarmChannelRegistryKey,
				sharedalarmkeys.ChzzkChannelMapKey,
				sharedalarmkeys.ChzzkChannelMapEmptyKey,
				sharedalarmkeys.TwitchLoginMapKey,
				sharedalarmkeys.TwitchLoginMapEmptyKey,
				sharedalarmkeys.TwitchChannelLoginMapKey,
				sharedalarmkeys.TwitchChannelLoginMapEmptyKey:
			default:
				t.Fatalf("unexpected key: %s", key)
			}
			return true, nil
		}

		s := &RuntimeScheduler{
			cacheClient:           cache,
			alarmCacheWarmer:      warmer,
			platformMappingSyncer: warmer,
			logger:                testSchedulerLogger(),
		}

		require.NoError(t, s.recoverAlarmCacheIfRegistryEmpty(t.Context(), "test"))
		assert.Equal(t, int32(0), warmer.calls.Load())
		assert.Equal(t, int32(0), warmer.syncCalls.Load())
	})

	t.Run("syncs platform mappings when registry exists but platform keys are missing", func(t *testing.T) {
		warmer := &alarmCacheWarmerStub{}
		cache := cachemocks.NewStrictClient()
		cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
			switch key {
			case sharedalarmkeys.AlarmChannelRegistryKey:
				return true, nil
			case sharedalarmkeys.ChzzkChannelMapKey:
				return false, nil
			case sharedalarmkeys.ChzzkChannelMapEmptyKey:
				return false, nil
			case sharedalarmkeys.TwitchLoginMapKey, sharedalarmkeys.TwitchChannelLoginMapKey:
				return true, nil
			default:
				t.Fatalf("unexpected key: %s", key)
				return false, nil
			}
		}

		s := &RuntimeScheduler{
			cacheClient:           cache,
			alarmCacheWarmer:      warmer,
			platformMappingSyncer: warmer,
			logger:                testSchedulerLogger(),
		}

		require.NoError(t, s.recoverAlarmCacheIfRegistryEmpty(t.Context(), "test"))
		assert.Equal(t, int32(0), warmer.calls.Load())
		assert.Equal(t, int32(1), warmer.syncCalls.Load())
	})

	t.Run("skips platform sync when missing mapping has empty marker", func(t *testing.T) {
		warmer := &alarmCacheWarmerStub{}
		cache := cachemocks.NewStrictClient()
		cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
			switch key {
			case sharedalarmkeys.AlarmChannelRegistryKey:
				return true, nil
			case sharedalarmkeys.ChzzkChannelMapKey:
				return false, nil
			case sharedalarmkeys.ChzzkChannelMapEmptyKey:
				return true, nil
			case sharedalarmkeys.TwitchLoginMapKey, sharedalarmkeys.TwitchChannelLoginMapKey:
				return true, nil
			default:
				t.Fatalf("unexpected key: %s", key)
				return false, nil
			}
		}

		s := &RuntimeScheduler{
			cacheClient:           cache,
			alarmCacheWarmer:      warmer,
			platformMappingSyncer: warmer,
			logger:                testSchedulerLogger(),
		}

		require.NoError(t, s.recoverAlarmCacheIfRegistryEmpty(t.Context(), "test"))
		assert.Equal(t, int32(0), warmer.calls.Load())
		assert.Equal(t, int32(0), warmer.syncCalls.Load())
	})
}

func TestRuntimeSchedulerRecoverAlarmCacheAfterCheckFailureUsesRecoveryTimeout(t *testing.T) {
	t.Parallel()

	warmer := &alarmCacheWarmerStub{}
	cache := cachemocks.NewStrictClient()
	cache.WaitUntilReadyFunc = func(ctx context.Context, _ time.Duration) error {
		_, ok := ctx.Deadline()
		require.True(t, ok)
		return nil
	}
	cache.ExistsFunc = func(ctx context.Context, key string) (bool, error) {
		_, ok := ctx.Deadline()
		require.True(t, ok)
		switch key {
		case sharedalarmkeys.AlarmChannelRegistryKey,
			sharedalarmkeys.AlarmSubscriberCacheEmptyKey:
		default:
			t.Fatalf("unexpected key: %s", key)
		}
		return false, nil
	}

	s := &RuntimeScheduler{
		cacheClient:           cache,
		alarmCacheWarmer:      warmer,
		platformMappingSyncer: warmer,
		logger:                testSchedulerLogger(),
	}

	err := s.recoverAlarmCacheAfterCheckFailure(
		context.Background(),
		sharedcache.NewCacheError("failed", "smembers", sharedalarmkeys.AlarmChannelRegistryKey, errors.New("EOF")),
	)
	require.NoError(t, err)
	assert.Equal(t, int32(1), warmer.calls.Load())
	assert.Equal(t, int32(1), warmer.syncCalls.Load())
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

		err := s.runLoop(ctx, "test", 10*time.Millisecond, 50*time.Millisecond, true, func(context.Context) error {
			calls.Add(1)
			return errors.New("expected failure branch")
		})
		require.ErrorIs(t, err, context.Canceled)
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

func TestInitialLoopDelay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		now            time.Time
		interval       time.Duration
		runImmediately bool
		want           time.Duration
	}{
		{
			name:           "immediate loop starts without delay",
			now:            time.Date(2026, time.April, 10, 10, 54, 1, 0, time.UTC),
			interval:       time.Minute,
			runImmediately: true,
			want:           0,
		},
		{
			name:           "youtube loop waits until next minute when off boundary",
			now:            time.Date(2026, time.April, 10, 10, 54, 1, 0, time.UTC),
			interval:       time.Minute,
			runImmediately: false,
			want:           59 * time.Second,
		},
		{
			name:           "youtube loop runs immediately on exact boundary",
			now:            time.Date(2026, time.April, 10, 10, 55, 0, 0, time.UTC),
			interval:       time.Minute,
			runImmediately: false,
			want:           0,
		},
		{
			name:           "non-positive interval falls back to immediate run",
			now:            time.Date(2026, time.April, 10, 10, 55, 1, 0, time.UTC),
			interval:       0,
			runImmediately: false,
			want:           0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, initialLoopDelay(tc.now, tc.interval, tc.runImmediately))
		})
	}
}
