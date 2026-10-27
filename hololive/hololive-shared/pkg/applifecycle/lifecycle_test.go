package applifecycle

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStart_StartsHTTPServerWithErrorChannel(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	called := false

	Start(t.Context(), errCh, StartHooks{
		Logger:     slog.New(slog.DiscardHandler),
		ServerAddr: "127.0.0.1:0",
		StartHTTPServer: func(gotErrCh chan<- error) {
			called = true

			if gotErrCh != chan<- error(errCh) {
				t.Fatal("StartHTTPServer received unexpected error channel")
			}
		},
	})

	require.True(t, called)
}

func TestStart_HandlesNilHooksAndNilContext(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		Start(t.Context(), make(chan error, 1), StartHooks{})
	})
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

func TestShutdown_CallsHTTPShutdownHook(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	calls := make([]string, 0, 1)

	httpErr := errors.New("http shutdown failed")
	err := Shutdown(ctx, ShutdownHooks{
		Logger: slog.New(slog.DiscardHandler),
		ShutdownHTTPServer: func(gotCtx context.Context) error {
			calls = append(calls, "shutdown-http-server")

			if gotCtx != ctx {
				t.Fatal("ShutdownHTTPServer received unexpected context")
			}

			return httpErr
		},
	})

	assert.Equal(t, []string{"shutdown-http-server"}, calls)
	assert.ErrorIs(t, err, httpErr)
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
