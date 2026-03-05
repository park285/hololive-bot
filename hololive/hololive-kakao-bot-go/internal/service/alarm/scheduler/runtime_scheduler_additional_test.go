package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/checker"
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

func testSchedulerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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
		tc := tc
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

		err := s.runYouTubeIteration(context.Background())
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

		err := s.runYouTubeIteration(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dispatch notifications: send notifications")
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

		require.NoError(t, s.runChzzkIteration(context.Background()))
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

		require.NoError(t, s.runTwitchIteration(context.Background()))
		assert.Equal(t, int32(0), sent.Load())
	})
}

func TestRuntimeSchedulerRunLoop_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	s := &RuntimeScheduler{
		logger: testSchedulerLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
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
