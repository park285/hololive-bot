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

package runtime

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lifecycleContextKey struct{}

func TestStart_RunsConfiguredHooks(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	ctx := context.WithValue(t.Context(), lifecycleContextKey{}, "parent")
	alarmCtxCh := make(chan context.Context, 1)
	configCtxCh := make(chan context.Context, 1)
	botCtxCh := make(chan context.Context, 1)
	cancelCh := make(chan context.CancelFunc, 1)
	order := make([]string, 0, 2)

	Start(ctx, errCh, StartHooks{
		Logger:     slog.New(slog.DiscardHandler),
		ServerAddr: "127.0.0.1:0",
		StartAlarmScheduler: func(ctx context.Context) {
			alarmCtxCh <- ctx
		},
		RunConfigSubscriber: func(ctx context.Context) {
			configCtxCh <- ctx
		},
		StartBot: func(ctx context.Context) error {
			botCtxCh <- ctx
			return nil
		},
		StartHTTPServer: func(gotErrCh chan<- error) {
			order = append(order, "http-server")
			if gotErrCh != chan<- error(errCh) {
				t.Fatal("StartHTTPServer received unexpected error channel")
			}
		},
		SetAlarmSchedulerCancel: func(cancel context.CancelFunc) {
			order = append(order, "set-alarm-cancel")
			cancelCh <- cancel
		},
	})

	require.Equal(t, []string{"set-alarm-cancel", "http-server"}, order)

	cancelAlarm := receiveLifecycleTestValue(t, cancelCh)
	alarmCtx := receiveLifecycleTestValue(t, alarmCtxCh)
	configCtx := receiveLifecycleTestValue(t, configCtxCh)
	botCtx := receiveLifecycleTestValue(t, botCtxCh)

	assert.Equal(t, "parent", alarmCtx.Value(lifecycleContextKey{}))
	assert.Equal(t, "parent", configCtx.Value(lifecycleContextKey{}))
	assert.Equal(t, "parent", botCtx.Value(lifecycleContextKey{}))

	cancelAlarm()
	select {
	case <-alarmCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("alarm scheduler context was not canceled")
	}
}

func TestStart_UsesParentContextWithoutAlarmCancelSetter(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(t.Context(), lifecycleContextKey{}, "parent")
	alarmCtxCh := make(chan context.Context, 1)

	Start(ctx, nil, StartHooks{
		Logger: slog.New(slog.DiscardHandler),
		StartAlarmScheduler: func(ctx context.Context) {
			alarmCtxCh <- ctx
		},
	})

	alarmCtx := receiveLifecycleTestValue(t, alarmCtxCh)
	assert.Equal(t, "parent", alarmCtx.Value(lifecycleContextKey{}))
}

func TestStart_HandlesNilHooksAndNilContext(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		Start(context.TODO(), make(chan error, 1), StartHooks{})
	})
}

func TestRun_DelegatesStartAndShutdown(t *testing.T) {
	t.Parallel()

	var startCalled atomic.Bool
	var shutdownCalled atomic.Bool

	Run(nil, func(_ context.Context, errCh chan<- error) {
		startCalled.Store(true)
		errCh <- errors.New("stop runtime")
	}, func(context.Context) {
		shutdownCalled.Store(true)
	})

	assert.True(t, startCalled.Load())
	assert.True(t, shutdownCalled.Load())
}

func TestShutdown_CallsHooksInOrderAndContinuesAfterErrors(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	calls := make([]string, 0, 5)

	Shutdown(ctx, ShutdownHooks{
		Logger: slog.New(slog.DiscardHandler),
		ClearAlarmScheduler: func() bool {
			calls = append(calls, "clear-alarm-scheduler")
			return true
		},
		ShutdownHTTPServer: func(gotCtx context.Context) error {
			calls = append(calls, "shutdown-http-server")
			if gotCtx != ctx {
				t.Fatal("ShutdownHTTPServer received unexpected context")
			}
			return errors.New("http shutdown failed")
		},
		WebhookHandlerClose: func() error {
			calls = append(calls, "close-webhook-handler")
			return errors.New("webhook close failed")
		},
		ShutdownAlarmServices: func(gotCtx context.Context) error {
			calls = append(calls, "shutdown-alarm-services")
			if gotCtx != ctx {
				t.Fatal("ShutdownAlarmServices received unexpected context")
			}
			return errors.New("alarm shutdown failed")
		},
		ShutdownBot: func(gotCtx context.Context) error {
			calls = append(calls, "shutdown-bot")
			if gotCtx != ctx {
				t.Fatal("ShutdownBot received unexpected context")
			}
			return errors.New("bot shutdown failed")
		},
	})

	assert.Equal(t, []string{
		"clear-alarm-scheduler",
		"shutdown-http-server",
		"close-webhook-handler",
		"shutdown-alarm-services",
		"shutdown-bot",
	}, calls)
}

func TestShutdown_HandlesNilHooks(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		Shutdown(context.TODO(), ShutdownHooks{})
	})
}

func TestLogHelpers_HandleNilLogger(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		logInfo(nil, "info", slog.String("key", "value"))
		logError(nil, "error", errors.New("boom"))
	})
}

func receiveLifecycleTestValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lifecycle hook")
	}

	var zero T
	return zero
}
