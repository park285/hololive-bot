package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedLogRecord struct {
	level   string
	message string
	attrs   map[string]any
}

type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

func newCaptureLogger() (*slog.Logger, *lockedLogBuffer) {
	buffer := &lockedLogBuffer{}
	return slog.New(slog.NewJSONHandler(buffer, nil)), buffer
}

func findRecord(buffer *lockedLogBuffer, message string) (capturedLogRecord, bool) {
	if buffer == nil {
		return capturedLogRecord{}, false
	}
	for line := range bytes.SplitSeq(bytes.TrimSpace(buffer.Bytes()), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		record, ok := decodeCapturedLogRecord(line)
		if ok && record.message == message {
			return record, true
		}
	}
	return capturedLogRecord{}, false
}

func decodeCapturedLogRecord(line []byte) (capturedLogRecord, bool) {
	var fields map[string]any
	if err := json.Unmarshal(line, &fields); err != nil {
		return capturedLogRecord{}, false
	}
	record := capturedLogRecord{attrs: make(map[string]any, len(fields))}
	for key, value := range fields {
		switch key {
		case slog.LevelKey:
			level, ok := value.(string)
			if !ok {
				return capturedLogRecord{}, false
			}
			record.level = level
		case slog.MessageKey:
			message, ok := value.(string)
			if !ok {
				return capturedLogRecord{}, false
			}
			record.message = message
		case slog.TimeKey:
			continue
		default:
			record.attrs[key] = value
		}
	}
	return record, true
}

// 주입된 logger로 Scheduler 라이프사이클 로그가 동일 레벨/메시지/속성으로 흐르는지 고정한다.
func TestScheduler_InjectedLogger_LifecycleLogs(t *testing.T) {
	logger, records := newCaptureLogger()
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0, Logger: logger})

	ctx, cancel := context.WithCancel(context.Background())
	scheduler.Start(ctx)
	cancel()
	scheduler.Stop()

	starting, ok := findRecord(records, "Scheduler starting")
	require.True(t, ok, "expected 'Scheduler starting' log on injected logger")
	assert.Equal(t, "INFO", starting.level)
	assert.InDelta(t, 1, starting.attrs["worker_count"], 0)

	stopped, ok := findRecord(records, "Scheduler stopped")
	require.True(t, ok, "expected 'Scheduler stopped' log on injected logger")
	assert.Equal(t, "INFO", stopped.level)
}

// nil Logger는 기존 동작(slog.Default 폴백)을 보존하여 패닉 없이 동작해야 한다.
func TestScheduler_NilLogger_FallsBackToDefault(t *testing.T) {
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0})
	require.NotNil(t, scheduler.logger)

	ctx, cancel := context.WithCancel(context.Background())
	scheduler.Start(ctx)
	cancel()
	scheduler.Stop()
}

// 등록 검증 실패 경로가 주입된 logger로 WARN을 남기는지 고정한다.
func TestScheduler_InjectedLogger_RegisterWarn(t *testing.T) {
	logger, records := newCaptureLogger()
	scheduler := NewScheduler(&SchedulerConfig{WorkerCount: 1, RequestInterval: 0, Logger: logger})

	scheduler.Register("", &togglePollerStub{name: "toggle"}, PriorityNormal, time.Minute)

	rec, ok := findRecord(records, "Skip invalid scheduler registration")
	require.True(t, ok, "expected register warn on injected logger")
	assert.Equal(t, "WARN", rec.level)
}
