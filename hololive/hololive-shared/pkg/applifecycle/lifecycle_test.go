package applifecycle

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
		StartAlarmScheduler: func(ctx context.Context) error {
			alarmCtxCh <- ctx
			return nil
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
		StartAlarmScheduler: func(ctx context.Context) error {
			alarmCtxCh <- ctx
			return nil
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

func TestStart_AlarmSchedulerErrorPropagatesToErrCh(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	schedulerCrash := errors.New("scheduler crashed")

	Start(t.Context(), errCh, StartHooks{
		Logger: slog.New(slog.DiscardHandler),
		StartAlarmScheduler: func(context.Context) error {
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

func TestStart_AlarmSchedulerContextCancellationIsNotFatal(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	Start(ctx, errCh, StartHooks{
		Logger: slog.New(slog.DiscardHandler),
		StartAlarmScheduler: func(context.Context) error {
			return context.Canceled
		},
	})

	select {
	case err := <-errCh:
		t.Fatalf("context cancellation must not be propagated as fatal error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStart_NonErrorAlarmAdapterDoesNotTouchErrCh(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	called := make(chan struct{}, 1)

	Start(t.Context(), errCh, StartHooks{
		Logger: slog.New(slog.DiscardHandler),
		StartAlarmScheduler: func(context.Context) error {
			called <- struct{}{}
			return nil
		},
	})

	receiveLifecycleTestValue(t, called)

	select {
	case err := <-errCh:
		t.Fatalf("non-error alarm adapter must not send to errCh: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStart_BotErrorPropagatesToErrCh(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	botCrash := errors.New("bot crashed")

	Start(t.Context(), errCh, StartHooks{
		Logger: slog.New(slog.DiscardHandler),
		StartBot: func(context.Context) error {
			return botCrash
		},
	})

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected bot error, got nil")
		}
		if !errors.Is(err, botCrash) {
			t.Fatalf("unexpected bot error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected bot error to be sent to errCh")
	}
}

func TestStart_BotContextCancellationIsNotFatal(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	Start(ctx, errCh, StartHooks{
		Logger: slog.New(slog.DiscardHandler),
		StartBot: func(context.Context) error {
			return context.Canceled
		},
	})

	select {
	case err := <-errCh:
		t.Fatalf("bot context cancellation must not be propagated as fatal error: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStartRunsH3CertReloadHookWithRunContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	gotCh := make(chan context.Context, 1)
	Start(ctx, nil, StartHooks{
		StartH3CertReload: func(c context.Context) { gotCh <- c },
	})

	got := receiveLifecycleTestValue(t, gotCh)
	if got != ctx {
		t.Fatalf("StartH3CertReload ctx = %v, want run ctx", got)
	}
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
