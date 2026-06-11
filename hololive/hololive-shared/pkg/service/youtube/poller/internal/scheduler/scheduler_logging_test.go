package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedLogRecord struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

type captureLogHandler struct {
	mu      sync.Mutex
	records *[]capturedLogRecord
}

func newCaptureLogger() (*slog.Logger, *[]capturedLogRecord) {
	records := &[]capturedLogRecord{}
	handler := &captureLogHandler{records: records}
	return slog.New(handler), records
}

func (h *captureLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureLogHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, capturedLogRecord{level: r.Level, message: r.Message, attrs: attrs})
	return nil
}

func (h *captureLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureLogHandler) WithGroup(string) slog.Handler      { return h }

func findRecord(records []capturedLogRecord, message string) (capturedLogRecord, bool) {
	for _, rec := range records {
		if rec.message == message {
			return rec, true
		}
	}
	return capturedLogRecord{}, false
}

// 주입된 logger로 Scheduler 라이프사이클 로그가 동일 레벨/메시지/속성으로 흐르는지 고정한다.
func TestScheduler_InjectedLogger_LifecycleLogs(t *testing.T) {
	logger, records := newCaptureLogger()
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0, Logger: logger})

	ctx, cancel := context.WithCancel(context.Background())
	scheduler.Start(ctx)
	cancel()
	scheduler.Stop()

	starting, ok := findRecord(*records, "Scheduler starting")
	require.True(t, ok, "expected 'Scheduler starting' log on injected logger")
	assert.Equal(t, slog.LevelInfo, starting.level)
	assert.Equal(t, int64(1), starting.attrs["worker_count"])

	stopped, ok := findRecord(*records, "Scheduler stopped")
	require.True(t, ok, "expected 'Scheduler stopped' log on injected logger")
	assert.Equal(t, slog.LevelInfo, stopped.level)
}

// nil Logger는 기존 동작(slog.Default 폴백)을 보존하여 패닉 없이 동작해야 한다.
func TestScheduler_NilLogger_FallsBackToDefault(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	require.NotNil(t, scheduler.logger)

	ctx, cancel := context.WithCancel(context.Background())
	scheduler.Start(ctx)
	cancel()
	scheduler.Stop()
}

// 등록 검증 실패 경로가 주입된 logger로 WARN을 남기는지 고정한다.
func TestScheduler_InjectedLogger_RegisterWarn(t *testing.T) {
	logger, records := newCaptureLogger()
	scheduler := NewScheduler(SchedulerConfig{WorkerCount: 1, RequestInterval: 0, Logger: logger})

	scheduler.Register("", &togglePollerStub{name: "toggle"}, PriorityNormal, time.Minute)

	rec, ok := findRecord(*records, "Skip invalid scheduler registration")
	require.True(t, ok, "expected register warn on injected logger")
	assert.Equal(t, slog.LevelWarn, rec.level)
}
