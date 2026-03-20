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

package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testWebhookCloser struct {
	calls int
	err   error
}

func (c *testWebhookCloser) Close() error {
	c.calls++
	return c.err
}

type testAlarmRuntimeScheduler struct {
	startedCh chan struct{}
	stoppedCh chan struct{}
	calls     atomic.Int32
	startOnce sync.Once
	stopOnce  sync.Once
}

func newTestAlarmRuntimeScheduler() *testAlarmRuntimeScheduler {
	return &testAlarmRuntimeScheduler{
		startedCh: make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

func (s *testAlarmRuntimeScheduler) Start(ctx context.Context) {
	s.calls.Add(1)
	s.startOnce.Do(func() { close(s.startedCh) })
	<-ctx.Done()
	s.stopOnce.Do(func() { close(s.stoppedCh) })
}

func waitForSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestBotRuntimeShutdown_ClosesWebhookHandler(t *testing.T) {
	webhookCloser := &testWebhookCloser{}
	runtime := &BotRuntime{
		webhookHandlerCloser: webhookCloser,
	}

	runtime.Shutdown(t.Context())

	if webhookCloser.calls != 1 {
		t.Fatalf("webhook Close calls = %d, want %d", webhookCloser.calls, 1)
	}
}

func TestBotRuntimeStartSchedulers_StartsAlarmRuntimeScheduler(t *testing.T) {
	var logBuf bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	alarmScheduler := newTestAlarmRuntimeScheduler()

	runtime := &BotRuntime{
		Logger:         logger,
		AlarmScheduler: alarmScheduler,
	}

	runtime.startSchedulers(t.Context(), nil)
	waitForSignal(t, alarmScheduler.startedCh, "alarm scheduler start")

	runtime.Shutdown(t.Context())
	waitForSignal(t, alarmScheduler.stoppedCh, "alarm scheduler stop")

	if got := alarmScheduler.calls.Load(); got != 1 {
		t.Fatalf("alarm scheduler Start calls = %d, want %d", got, 1)
	}

	if !strings.Contains(logBuf.String(), "Alarm runtime scheduler started") {
		t.Fatalf("log does not contain start message: %s", logBuf.String())
	}
}

func TestBotRuntimeStartSchedulers_LogsWhenAlarmRuntimeSchedulerMissing(t *testing.T) {
	var logBuf bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	runtime := &BotRuntime{
		Logger: logger,
	}

	runtime.startSchedulers(t.Context(), nil)

	if !strings.Contains(logBuf.String(), "Alarm runtime scheduler not configured") {
		t.Fatalf("log does not contain unconfigured message: %s", logBuf.String())
	}
}

func TestBotRuntimeShutdown_CancelsAlarmRuntimeSchedulerOnCanceledContext(t *testing.T) {
	alarmScheduler := newTestAlarmRuntimeScheduler()
	runtime := &BotRuntime{
		AlarmScheduler: alarmScheduler,
	}

	runtime.startSchedulers(t.Context(), nil)
	waitForSignal(t, alarmScheduler.startedCh, "alarm scheduler start")

	shutdownCtx, cancel := context.WithCancel(t.Context())
	cancel()

	runtime.Shutdown(shutdownCtx)
	waitForSignal(t, alarmScheduler.stoppedCh, "alarm scheduler stop")
}
