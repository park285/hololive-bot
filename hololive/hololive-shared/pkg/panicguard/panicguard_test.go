package panicguard

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testErrorGroup struct {
	err chan error
	wg  sync.WaitGroup
}

func (g *testErrorGroup) Go(fn func() error) {
	g.wg.Go(func() {
		g.err <- fn()
	})
}

func bufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	return logger, &buf
}

func TestRun_RecoversPanicAndLogs(t *testing.T) {
	t.Parallel()

	logger, buf := bufferLogger()

	require.NotPanics(t, func() {
		Run(logger, "guarded-loop", func() {
			panic("boom")
		})
	})

	out := buf.String()
	assert.Contains(t, out, "level=ERROR")
	assert.Contains(t, out, "guard=guarded-loop")
	assert.Contains(t, out, "boom")
	assert.Contains(t, out, "stack=")
}

func TestRun_NoPanicRunsFn(t *testing.T) {
	t.Parallel()

	logger, buf := bufferLogger()
	ran := false

	Run(logger, "ok", func() { ran = true })

	assert.True(t, ran)
	assert.Empty(t, buf.String())
}

func TestRunE_ReturnsFnErrorUnchanged(t *testing.T) {
	t.Parallel()

	logger, buf := bufferLogger()
	sentinel := errors.New("fn failed")

	err := RunE(logger, "ok", func() error { return sentinel })

	assert.ErrorIs(t, err, sentinel)
	assert.Empty(t, buf.String())
}

func TestRunE_ConvertsStringPanicToError(t *testing.T) {
	t.Parallel()

	logger, buf := bufferLogger()

	var err error
	require.NotPanics(t, func() {
		err = RunE(logger, "alarm-scheduler", func() error {
			panic("kaboom")
		})
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "alarm-scheduler")
	assert.Contains(t, err.Error(), "kaboom")
	assert.Contains(t, buf.String(), "guard=alarm-scheduler")
}

func TestRunE_PreservesPanickedErrorChain(t *testing.T) {
	t.Parallel()

	logger, _ := bufferLogger()
	sentinel := errors.New("panicked error")

	err := RunE(logger, "bot-runtime", func() error {
		panic(sentinel)
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func TestLogPanic_NilLoggerIsSafe(t *testing.T) {
	t.Parallel()

	require.NotPanics(t, func() {
		Run(nil, "no-logger", func() { panic("x") })
		assert.Error(t, RunE(nil, "no-logger", func() error { panic("y") }))
	})
}

func TestRun_StackIncludesGoroutineFrame(t *testing.T) {
	t.Parallel()

	logger, buf := bufferLogger()
	Run(logger, "stack-check", func() { panic("trace me") })

	assert.True(t, strings.Contains(buf.String(), "panicguard.Run"),
		"stack should reference the guard frame, got: %s", buf.String())
}

func TestGo_StartsGuardedGoroutine(t *testing.T) {
	t.Parallel()

	logger, buf := bufferLogger()
	done := make(chan struct{})
	Go(logger, "async", func() {
		close(done)
	})
	<-done

	assert.Empty(t, buf.String())
}

func TestGoE_ReportsRecoveredPanicToGroup(t *testing.T) {
	t.Parallel()

	logger, buf := bufferLogger()
	group := &testErrorGroup{err: make(chan error, 1)}
	GoE(group, logger, "grouped", func() error {
		panic("group panic")
	})

	err := <-group.err
	group.wg.Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group panic")
	assert.Contains(t, buf.String(), "guard=grouped")
}
