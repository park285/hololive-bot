package applifecycle

import (
	"bytes"
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
		Start(t.Context(), make(chan error, 1), StartHooks{})
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

func TestStart_AlarmSchedulerPanicIsRecoveredAndPropagatedToErrCh(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)

	require.NotPanics(t, func() {
		Start(t.Context(), errCh, StartHooks{
			Logger: slog.New(slog.DiscardHandler),
			StartAlarmScheduler: func(context.Context) error {
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

func TestStart_BotPanicIsRecoveredAndPropagatedToErrCh(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)

	require.NotPanics(t, func() {
		Start(t.Context(), errCh, StartHooks{
			Logger: slog.New(slog.DiscardHandler),
			StartBot: func(context.Context) error {
				panic("bot exploded")
			},
		})
	})

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bot exploded")
	case <-time.After(time.Second):
		t.Fatal("expected recovered bot panic to be sent to errCh")
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

	var (
		startCalled    atomic.Bool
		shutdownCalled atomic.Bool
	)

	runtimeErr := errors.New("stop runtime")

	err := Run(nil, func(_ context.Context, errCh chan<- error) {
		startCalled.Store(true)

		errCh <- runtimeErr
	}, func(context.Context) error {
		shutdownCalled.Store(true)

		return nil
	})

	require.ErrorIs(t, err, runtimeErr)
	assert.True(t, startCalled.Load())
	assert.True(t, shutdownCalled.Load())
}

func TestRun_LogsRuntimeAndShutdownErrorsAtTheirOwners(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		runtimeErr      error
		shutdownErr     error
		wantShutdownLog bool
	}{
		{name: "runtime", runtimeErr: errors.New("runtime failed")},
		{name: "both", runtimeErr: errors.New("runtime failed"), shutdownErr: errors.New("shutdown failed"), wantShutdownLog: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer

			logger := slog.New(slog.NewTextHandler(&output, nil))
			err := Run(logger, func(_ context.Context, errCh chan<- error) {
				errCh <- tc.runtimeErr
			}, func(context.Context) error {
				return tc.shutdownErr
			})

			if tc.runtimeErr != nil {
				require.ErrorIs(t, err, tc.runtimeErr)
			}

			if tc.shutdownErr != nil {
				require.ErrorIs(t, err, tc.shutdownErr)
			}

			assert.True(t, bytes.Contains(output.Bytes(), []byte("Server error")))
			assert.Equal(t, tc.wantShutdownLog, bytes.Contains(output.Bytes(), []byte("Shutdown error")))
		})
	}
}

func TestRuntimeOptions_LogsShutdownError(t *testing.T) {
	t.Parallel()

	shutdownErr := errors.New("shutdown failed")

	var output bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&output, nil))
	opts := runtimeOptions(logger, func(context.Context, chan<- error) {}, func(context.Context) error {
		return shutdownErr
	})

	err := opts.Shutdown(t.Context())
	require.ErrorIs(t, err, shutdownErr)
	assert.True(t, bytes.Contains(output.Bytes(), []byte("Shutdown error")))
}

func TestShutdown_CallsHooksInOrderAndContinuesAfterErrors(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	calls := make([]string, 0, 5)

	httpErr := errors.New("http shutdown failed")
	webhookErr := errors.New("webhook close failed")
	alarmErr := errors.New("alarm shutdown failed")
	botErr := errors.New("bot shutdown failed")
	err := Shutdown(ctx, ShutdownHooks{
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

			return httpErr
		},
		WebhookHandlerClose: func() error {
			calls = append(calls, "close-webhook-handler")
			return webhookErr
		},
		ShutdownAlarmServices: func(gotCtx context.Context) error {
			calls = append(calls, "shutdown-alarm-services")

			if gotCtx != ctx {
				t.Fatal("ShutdownAlarmServices received unexpected context")
			}

			return alarmErr
		},
		ShutdownBot: func(gotCtx context.Context) error {
			calls = append(calls, "shutdown-bot")

			if gotCtx != ctx {
				t.Fatal("ShutdownBot received unexpected context")
			}

			return botErr
		},
	})

	assert.Equal(t, []string{
		"clear-alarm-scheduler",
		"shutdown-http-server",
		"close-webhook-handler",
		"shutdown-alarm-services",
		"shutdown-bot",
	}, calls)

	for _, wantErr := range []error{httpErr, webhookErr, alarmErr, botErr} {
		assert.ErrorIs(t, err, wantErr)
	}
}

func TestShutdown_HandlesNilHooks(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		require.NoError(t, Shutdown(t.Context(), ShutdownHooks{}))
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
