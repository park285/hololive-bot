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

	runtime.Shutdown(context.Background())

	if webhookCloser.calls != 1 {
		t.Fatalf("webhook Close calls = %d, want %d", webhookCloser.calls, 1)
	}
}

func TestBotRuntimeStartSchedulers_StartsAlarmRuntimeScheduler(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	alarmScheduler := newTestAlarmRuntimeScheduler()

	runtime := &BotRuntime{
		Logger:           logger,
		IngestionEnabled: true,
		AlarmScheduler:   alarmScheduler,
	}

	runtime.startSchedulers(context.Background(), nil)
	waitForSignal(t, alarmScheduler.startedCh, "alarm scheduler start")

	runtime.Shutdown(context.Background())
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
		Logger:           logger,
		IngestionEnabled: true,
	}

	runtime.startSchedulers(context.Background(), nil)

	if !strings.Contains(logBuf.String(), "Alarm runtime scheduler not configured") {
		t.Fatalf("log does not contain unconfigured message: %s", logBuf.String())
	}
}

func TestBotRuntimeShutdown_CancelsAlarmRuntimeSchedulerOnCanceledContext(t *testing.T) {
	alarmScheduler := newTestAlarmRuntimeScheduler()
	runtime := &BotRuntime{
		IngestionEnabled: true,
		AlarmScheduler:   alarmScheduler,
	}

	runtime.startSchedulers(context.Background(), nil)
	waitForSignal(t, alarmScheduler.startedCh, "alarm scheduler start")

	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()

	runtime.Shutdown(shutdownCtx)
	waitForSignal(t, alarmScheduler.stoppedCh, "alarm scheduler stop")
}
