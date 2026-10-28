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

package workerruntime

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type schedulerContextKey struct{}

func receiveSchedulerTestValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for scheduler hook")
	}

	var zero T

	return zero
}

func TestAlarmScheduler_RunsConfiguredHooks(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	ctx := context.WithValue(t.Context(), schedulerContextKey{}, "parent")
	alarmCtxCh := make(chan context.Context, 1)
	cancelCh := make(chan context.CancelFunc, 1)
	order := make([]string, 0, 2)

	startAlarmScheduler(ctx, errCh, alarmSchedulerHooks{
		Logger: slog.New(slog.DiscardHandler),
		Start: func(ctx context.Context) error {
			alarmCtxCh <- ctx
			return nil
		},
		SetCancel: func(cancel context.CancelFunc) {
			order = append(order, "set-alarm-cancel")

			cancelCh <- cancel
		},
	})

	require.Equal(t, []string{"set-alarm-cancel"}, order)

	cancelAlarm := receiveSchedulerTestValue(t, cancelCh)
	alarmCtx := receiveSchedulerTestValue(t, alarmCtxCh)

	assert.Equal(t, "parent", alarmCtx.Value(schedulerContextKey{}))

	cancelAlarm()

	select {
	case <-alarmCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("alarm scheduler context was not canceled")
	}
}

func TestAlarmScheduler_UsesParentContextWithoutAlarmCancelSetter(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(t.Context(), schedulerContextKey{}, "parent")
	alarmCtxCh := make(chan context.Context, 1)

	startAlarmScheduler(ctx, nil, alarmSchedulerHooks{
		Logger: slog.New(slog.DiscardHandler),
		Start: func(ctx context.Context) error {
			alarmCtxCh <- ctx
			return nil
		},
	})

	alarmCtx := receiveSchedulerTestValue(t, alarmCtxCh)
	assert.Equal(t, "parent", alarmCtx.Value(schedulerContextKey{}))
}

func TestAlarmScheduler_AlarmSchedulerErrorPropagatesToErrCh(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	schedulerCrash := errors.New("scheduler crashed")

	startAlarmScheduler(t.Context(), errCh, alarmSchedulerHooks{
		Logger: slog.New(slog.DiscardHandler),
		Start: func(context.Context) error {
			return schedulerCrash
		},
	})

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected scheduler error, got nil")
		}

		if !errors.Is(err, schedulerCrash) {
			t.Fatalf("unexpected scheduler error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected scheduler error to be sent to errCh")
	}
}

func TestAlarmScheduler_AlarmSchedulerContextCancellationIsNotFatal(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	startAlarmScheduler(ctx, errCh, alarmSchedulerHooks{
		Logger: slog.New(slog.DiscardHandler),
		Start: func(context.Context) error {
			return context.Canceled
		},
	})

	select {
	case err := <-errCh:
		t.Fatalf("context cancellation must not be propagated as fatal error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAlarmScheduler_NonErrorAlarmAdapterDoesNotTouchErrCh(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	called := make(chan struct{}, 1)

	startAlarmScheduler(t.Context(), errCh, alarmSchedulerHooks{
		Logger: slog.New(slog.DiscardHandler),
		Start: func(context.Context) error {
			called <- struct{}{}
			return nil
		},
	})

	receiveSchedulerTestValue(t, called)

	select {
	case err := <-errCh:
		t.Fatalf("non-error alarm adapter must not send to errCh: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAlarmScheduler_AlarmSchedulerPanicIsRecoveredAndPropagatedToErrCh(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)

	require.NotPanics(t, func() {
		startAlarmScheduler(t.Context(), errCh, alarmSchedulerHooks{
			Logger: slog.New(slog.DiscardHandler),
			Start: func(context.Context) error {
				panic("alarm scheduler exploded")
			},
		})
	})

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "alarm scheduler exploded")
	case <-time.After(time.Second):
		t.Fatal("expected recovered scheduler panic to be sent to errCh")
	}
}
