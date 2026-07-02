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

package lifecycle

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/workerpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCache struct {
	waitErr    error
	closeErr   error
	closeCalls int
}

func (c *stubCache) Close() error {
	c.closeCalls++
	return c.closeErr
}

func (c *stubCache) IsConnected(context.Context) bool { return true }

func (c *stubCache) WaitUntilReady(context.Context, time.Duration) error {
	return c.waitErr
}

type stubPinger struct {
	results []bool
	result  bool
	calls   int
	onCall  func(call int)
}

func (p *stubPinger) Ping(context.Context) bool {
	p.calls++
	if p.onCall != nil {
		p.onCall(p.calls)
	}
	if len(p.results) > 0 {
		r := p.results[0]
		p.results = p.results[1:]
		return r
	}
	return p.result
}

type stubStoppable struct {
	stopped   bool
	stopCalls int
}

func (s *stubStoppable) Stop() {
	s.stopped = true
	s.stopCalls++
}

type stubPostgres struct {
	closeErr   error
	closeCalls int
}

func (p *stubPostgres) GetPool() *pgxpool.Pool     { return nil }
func (p *stubPostgres) Ping(context.Context) error { return nil }

func (p *stubPostgres) Close() error {
	p.closeCalls++
	return p.closeErr
}

type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error { //nolint:gocritic // hugeParam: slog.Handler.Handle 인터페이스가 값 전달 시그니처를 강제
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.records))
	for i := range h.records {
		out = append(out, h.records[i].Message)
	}
	return out
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func assertChannelClosed(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	default:
		t.Fatal("expected channel to be closed")
	}
}

func TestNewBotLifecycle(t *testing.T) {
	t.Parallel()

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	cacheClient := &stubCache{}
	pinger := &stubPinger{}

	l := NewBotLifecycle(discardLogger(), cacheClient, pinger, "http://iris", stopCh, doneCh, nil, nil, nil)

	require.NotNil(t, l)
	assert.Equal(t, "http://iris", l.irisBaseURL)
	assert.Same(t, cacheClient, l.cache)
	assert.Same(t, pinger, l.irisClient)
	assert.Equal(t, stopCh, l.stopCh)
	assert.Equal(t, doneCh, l.doneCh)
}

func TestBotLifecycleStart(t *testing.T) {
	t.Parallel()

	t.Run("cache not configured", func(t *testing.T) {
		l := NewBotLifecycle(discardLogger(), nil, &stubPinger{}, "", make(chan struct{}), make(chan struct{}), nil, nil, nil)
		err := l.Start(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache is not configured")
	})

	t.Run("cache readiness failure", func(t *testing.T) {
		l := NewBotLifecycle(discardLogger(), &stubCache{waitErr: errors.New("down")}, &stubPinger{}, "", make(chan struct{}), make(chan struct{}), nil, nil, nil)
		err := l.Start(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "valkey connection timeout")
	})

	t.Run("nil iris client enters degraded mode then honors stop signal", func(t *testing.T) {
		stopCh := make(chan struct{})
		close(stopCh)
		l := NewBotLifecycle(discardLogger(), &stubCache{}, nil, "http://iris", stopCh, make(chan struct{}), nil, nil, nil)
		require.NoError(t, l.Start(t.Context()))
	})

	t.Run("iris ready then honors stop signal", func(t *testing.T) {
		stopCh := make(chan struct{})
		close(stopCh)
		l := NewBotLifecycle(discardLogger(), &stubCache{}, &stubPinger{result: true}, "http://iris", stopCh, make(chan struct{}), nil, nil, nil)
		require.NoError(t, l.Start(t.Context()))
	})

	t.Run("canceled context returns error", func(t *testing.T) {
		l := NewBotLifecycle(discardLogger(), &stubCache{}, &stubPinger{result: true}, "http://iris", make(chan struct{}), make(chan struct{}), nil, nil, nil)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := l.Start(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})
}

func TestBotLifecycleShutdown(t *testing.T) {
	t.Parallel()

	t.Run("all nil components closes done channel", func(t *testing.T) {
		doneCh := make(chan struct{})
		l := NewBotLifecycle(discardLogger(), nil, nil, "", make(chan struct{}), doneCh, nil, nil, nil)
		require.NoError(t, l.Shutdown(t.Context()))
		assertChannelClosed(t, doneCh)
	})

	t.Run("stops and closes all components", func(t *testing.T) {
		cacheClient := &stubCache{}
		holodex := &stubStoppable{}
		postgres := &stubPostgres{}
		pool := workerpool.NewQueued(workerpool.QueuedConfig{Workers: 1, QueueSize: 1})
		doneCh := make(chan struct{})

		l := NewBotLifecycle(discardLogger(), cacheClient, &stubPinger{}, "http://iris", make(chan struct{}), doneCh, pool, holodex, postgres)

		require.NoError(t, l.Shutdown(t.Context()))
		assert.Equal(t, 1, cacheClient.closeCalls)
		assert.True(t, holodex.stopped)
		assert.Equal(t, 1, postgres.closeCalls)
		assertChannelClosed(t, doneCh)
	})

	t.Run("component close errors are swallowed", func(t *testing.T) {
		cacheClient := &stubCache{closeErr: errors.New("cache boom")}
		postgres := &stubPostgres{closeErr: errors.New("pg boom")}
		doneCh := make(chan struct{})

		l := NewBotLifecycle(discardLogger(), cacheClient, nil, "", make(chan struct{}), doneCh, nil, nil, postgres)

		require.NoError(t, l.Shutdown(t.Context()))
		assert.Equal(t, 1, cacheClient.closeCalls)
		assert.Equal(t, 1, postgres.closeCalls)
		assertChannelClosed(t, doneCh)
	})

	t.Run("done channel closed only once across repeated shutdowns", func(t *testing.T) {
		cacheClient := &stubCache{}
		doneCh := make(chan struct{})
		l := NewBotLifecycle(discardLogger(), cacheClient, nil, "", make(chan struct{}), doneCh, nil, nil, nil)

		require.NoError(t, l.Shutdown(t.Context()))
		require.NoError(t, l.Shutdown(t.Context()))
		assert.Equal(t, 2, cacheClient.closeCalls)
		assertChannelClosed(t, doneCh)
	})

	t.Run("nil done channel does not panic", func(t *testing.T) {
		l := NewBotLifecycle(discardLogger(), nil, nil, "", make(chan struct{}), nil, nil, nil, nil)
		require.NoError(t, l.Shutdown(t.Context()))
	})
}

func TestWaitUntilIrisReady(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver returns configuration error", func(t *testing.T) {
		var l *BotLifecycle
		err := l.WaitUntilIrisReady(t.Context(), time.Second, time.Millisecond, time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("nil client returns configuration error", func(t *testing.T) {
		l := NewBotLifecycle(discardLogger(), nil, nil, "", nil, nil, nil, nil, nil)
		err := l.WaitUntilIrisReady(t.Context(), time.Second, time.Millisecond, time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "iris client is not configured")
	})

	t.Run("ping success on first attempt", func(t *testing.T) {
		p := &stubPinger{result: true}
		l := NewBotLifecycle(discardLogger(), nil, p, "", nil, nil, nil, nil, nil)
		require.NoError(t, l.WaitUntilIrisReady(t.Context(), time.Second, 10*time.Millisecond, 10*time.Millisecond))
		assert.Equal(t, 1, p.calls)
	})

	t.Run("timeout when ping never succeeds", func(t *testing.T) {
		p := &stubPinger{result: false}
		l := NewBotLifecycle(discardLogger(), nil, p, "", nil, nil, nil, nil, nil)
		err := l.WaitUntilIrisReady(t.Context(), 40*time.Millisecond, 10*time.Millisecond, 5*time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout after 40ms")
	})
}

func TestRunIrisReadyWaitLoop(t *testing.T) {
	t.Parallel()

	t.Run("ping success on first attempt returns nil", func(t *testing.T) {
		p := &stubPinger{result: true}
		l := NewBotLifecycle(discardLogger(), nil, p, "", nil, nil, nil, nil, nil)
		require.NoError(t, l.runIrisReadyWaitLoop(t.Context(), make(chan time.Time), time.Minute, time.Second, time.Second))
		assert.Equal(t, 1, p.calls)
	})

	t.Run("ping fails then succeeds after tick", func(t *testing.T) {
		p := &stubPinger{results: []bool{false, true}}
		l := NewBotLifecycle(discardLogger(), nil, p, "", nil, nil, nil, nil, nil)
		tick := make(chan time.Time, 1)
		tick <- time.Now()
		require.NoError(t, l.runIrisReadyWaitLoop(t.Context(), tick, time.Minute, time.Second, time.Second))
		assert.Equal(t, 2, p.calls)
	})

	t.Run("canceled context returns canceled error", func(t *testing.T) {
		p := &stubPinger{result: false}
		l := NewBotLifecycle(discardLogger(), nil, p, "", nil, nil, nil, nil, nil)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := l.runIrisReadyWaitLoop(ctx, make(chan time.Time), time.Minute, time.Second, time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "canceled")
	})
}

func TestValidateIrisReadyWaiter(t *testing.T) {
	t.Parallel()

	var nilReceiver *BotLifecycle
	require.Error(t, nilReceiver.validateIrisReadyWaiter())

	nilClient := NewBotLifecycle(discardLogger(), nil, nil, "", nil, nil, nil, nil, nil)
	require.Error(t, nilClient.validateIrisReadyWaiter())

	valid := NewBotLifecycle(discardLogger(), nil, &stubPinger{}, "", nil, nil, nil, nil, nil)
	require.NoError(t, valid.validateIrisReadyWaiter())
}

func TestPingIrisReady(t *testing.T) {
	t.Parallel()

	up := &stubPinger{result: true}
	l := NewBotLifecycle(discardLogger(), nil, up, "", nil, nil, nil, nil, nil)
	assert.True(t, l.pingIrisReady(t.Context(), 10*time.Millisecond))
	assert.Equal(t, 1, up.calls)

	down := &stubPinger{result: false}
	l2 := NewBotLifecycle(discardLogger(), nil, down, "", nil, nil, nil, nil, nil)
	assert.False(t, l2.pingIrisReady(t.Context(), 10*time.Millisecond))
}

func TestLogIrisReadyAfterRetry(t *testing.T) {
	t.Parallel()

	t.Run("attempt at most one logs nothing", func(t *testing.T) {
		h := &recordingHandler{}
		l := NewBotLifecycle(slog.New(h), nil, nil, "", nil, nil, nil, nil, nil)
		l.logIrisReadyAfterRetry(1, time.Now())
		assert.Empty(t, h.messages())
	})

	t.Run("attempt greater than one logs became-ready message", func(t *testing.T) {
		h := &recordingHandler{}
		l := NewBotLifecycle(slog.New(h), nil, nil, "", nil, nil, nil, nil, nil)
		l.logIrisReadyAfterRetry(3, time.Now().Add(-time.Second))
		require.Len(t, h.messages(), 1)
		assert.Equal(t, "Iris server became ready after retry", h.messages()[0])
	})
}

func TestLogIrisNotReadyRetry(t *testing.T) {
	t.Parallel()

	start := time.Now()

	t.Run("first attempt logs and returns fresh timestamp", func(t *testing.T) {
		l := NewBotLifecycle(discardLogger(), nil, nil, "", nil, nil, nil, nil, nil)
		got, ok := l.logIrisNotReadyRetry(1, time.Second, start, time.Time{})
		assert.True(t, ok)
		assert.False(t, got.IsZero())
	})

	t.Run("later attempt within a minute is suppressed", func(t *testing.T) {
		l := NewBotLifecycle(discardLogger(), nil, nil, "", nil, nil, nil, nil, nil)
		last := time.Now()
		got, ok := l.logIrisNotReadyRetry(2, time.Second, start, last)
		assert.False(t, ok)
		assert.Equal(t, last, got)
	})

	t.Run("later attempt after a minute logs again", func(t *testing.T) {
		l := NewBotLifecycle(discardLogger(), nil, nil, "", nil, nil, nil, nil, nil)
		last := time.Now().Add(-2 * time.Minute)
		got, ok := l.logIrisNotReadyRetry(2, time.Second, start, last)
		assert.True(t, ok)
		assert.True(t, got.After(last))
	})

	t.Run("first attempt bypasses throttle even with recent timestamp", func(t *testing.T) {
		l := NewBotLifecycle(discardLogger(), nil, nil, "", nil, nil, nil, nil, nil)
		_, ok := l.logIrisNotReadyRetry(1, time.Second, start, time.Now())
		assert.True(t, ok)
	})
}

func TestWaitNextIrisReadyRetry(t *testing.T) {
	t.Parallel()

	t.Run("tick returns nil", func(t *testing.T) {
		tick := make(chan time.Time, 1)
		tick <- time.Now()
		require.NoError(t, waitNextIrisReadyRetry(t.Context(), tick, time.Minute))
	})

	t.Run("canceled context returns canceled error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := waitNextIrisReadyRetry(ctx, make(chan time.Time), time.Minute)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "canceled")
	})

	t.Run("deadline exceeded returns timeout error", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
		defer cancel()
		<-ctx.Done()
		err := waitNextIrisReadyRetry(ctx, make(chan time.Time), 7*time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout after 7s")
	})
}

func TestIrisReadyWaitErr(t *testing.T) {
	t.Parallel()

	timeoutErr := irisReadyWaitErr(context.DeadlineExceeded, 5*time.Second)
	require.Error(t, timeoutErr)
	assert.Contains(t, timeoutErr.Error(), "timeout after 5s")

	canceledErr := irisReadyWaitErr(context.Canceled, 5*time.Second)
	require.Error(t, canceledErr)
	assert.Contains(t, canceledErr.Error(), "canceled")
	assert.ErrorIs(t, canceledErr, context.Canceled)
}

func TestLoggingHelpersAreNilSafe(t *testing.T) {
	t.Parallel()

	var nilReceiver *BotLifecycle
	require.NotPanics(t, func() {
		nilReceiver.logInfo("x")
		nilReceiver.logWarn("y")
	})

	nilLogger := NewBotLifecycle(nil, nil, nil, "", nil, nil, nil, nil, nil)
	require.NotPanics(t, func() {
		nilLogger.logInfo("x", slog.String("k", "v"))
		nilLogger.logWarn("y", slog.String("k", "v"))
	})
}
