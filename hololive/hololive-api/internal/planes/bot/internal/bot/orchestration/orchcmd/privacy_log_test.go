package orchcmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"

	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
)

const privacySentinel = "SENTINEL"

type capturedLogRecord struct {
	message string
	attrs   map[string]any
}

type capturingHandler struct {
	mu      *sync.Mutex
	records *[]capturedLogRecord
}

func (h capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h capturingHandler) Handle(_ context.Context, record slog.Record) error { //nolint:gocritic // hugeParam: slog.Handler.Handle 인터페이스가 값 전달 시그니처를 강제
	captured := capturedLogRecord{message: record.Message, attrs: make(map[string]any, record.NumAttrs())}
	record.Attrs(func(attr slog.Attr) bool {
		captured.attrs[attr.Key] = attr.Value.Any()
		return true
	})

	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, captured)

	return nil
}

func (h capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h capturingHandler) WithGroup(string) slog.Handler { return h }

func newCapturingLogger() (logger *slog.Logger, snapshot func() []capturedLogRecord) {
	var (
		mu      sync.Mutex
		records []capturedLogRecord
	)

	logger = slog.New(capturingHandler{mu: &mu, records: &records})
	snapshot = func() []capturedLogRecord {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedLogRecord(nil), records...)
	}

	return logger, snapshot
}

func requireEvent(t *testing.T, records []capturedLogRecord, event string) capturedLogRecord {
	t.Helper()

	for _, record := range records {
		if record.attrs["event"] == event {
			return record
		}
	}

	t.Fatalf("event %q must be emitted, got records %#v", event, records)
	return capturedLogRecord{}
}

func assertNoLogSubstring(t *testing.T, records []capturedLogRecord, needle string) {
	t.Helper()

	for _, record := range records {
		if strings.Contains(record.message, needle) {
			t.Fatalf("log message leaked %q in record %#v", needle, record)
		}
		for key, value := range record.attrs {
			if strings.Contains(fmt.Sprint(value), needle) {
				t.Fatalf("log attr %s leaked %q in record %#v", key, needle, record)
			}
		}
	}
}

func assertNoLogAttrKey(t *testing.T, records []capturedLogRecord, key string) {
	t.Helper()

	for _, record := range records {
		if _, ok := record.attrs[key]; ok {
			t.Fatalf("log attr %q must not be emitted, found in record %#v", key, record)
		}
	}
}

func newPrivacyCommandContext() *domain.CommandContext {
	threadID := "thread-1"
	return &domain.CommandContext{
		Room:     "room-1",
		RoomName: "룸이름-" + privacySentinel,
		UserID:   "user-1",
		UserName: "닉네임-" + privacySentinel,
		Message:  "!help 본문-" + privacySentinel,
		ThreadID: &threadID,
	}
}

func assertPrivacyAttrs(t *testing.T, record capturedLogRecord) {
	t.Helper()

	if record.attrs["room_id"] != "room-1" {
		t.Fatalf("room_id = %v, want %q", record.attrs["room_id"], "room-1")
	}
	if record.attrs["user_id"] != "user-1" {
		t.Fatalf("user_id = %v, want %q", record.attrs["user_id"], "user-1")
	}
	if record.attrs["thread_id"] != "thread-1" {
		t.Fatalf("thread_id = %v, want %q", record.attrs["thread_id"], "thread-1")
	}
}

func TestCommandRouterExecutionLogsKeepEventsWithoutContentOrNickname(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register(&trackedRouterCommand{name: "help"})
	logger, snapshot := newCapturingLogger()
	router := NewCommandRouter(registry, logger, func(context.Context, string, string) error { return nil }, nil, nil)

	if err := router.Execute(t.Context(), newPrivacyCommandContext(), domain.CommandHelp, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	records := snapshot()
	assertPrivacyAttrs(t, requireEvent(t, records, EventBotCommandExecuteStarted))
	assertPrivacyAttrs(t, requireEvent(t, records, EventBotCommandExecuteSucceeded))

	assertNoLogSubstring(t, records, privacySentinel)
	assertNoLogAttrKey(t, records, "message_sha256_8")
	assertNoLogAttrKey(t, records, "user_name")
	assertNoLogAttrKey(t, records, "room_name")
}

func TestCommandRouterFailureLogKeepsEventWithoutContentOrNickname(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register(&failingRouterCommand{name: "help", err: errors.New("handler down")})
	logger, snapshot := newCapturingLogger()
	router := NewCommandRouter(registry, logger, func(context.Context, string, string) error { return nil }, nil, nil)

	err := router.Execute(t.Context(), newPrivacyCommandContext(), domain.CommandHelp, nil)
	if err == nil {
		t.Fatal("Execute() error = nil, want handler failure")
	}
	if strings.Contains(err.Error(), privacySentinel) {
		t.Fatalf("returned error leaked sentinel: %v", err)
	}

	records := snapshot()
	assertPrivacyAttrs(t, requireEvent(t, records, EventBotCommandExecuteFailed))

	assertNoLogSubstring(t, records, privacySentinel)
	assertNoLogAttrKey(t, records, "message_sha256_8")
	assertNoLogAttrKey(t, records, "user_name")
	assertNoLogAttrKey(t, records, "room_name")
}

type failingRouterCommand struct {
	name string
	err  error
}

func (c *failingRouterCommand) Name() string        { return c.name }
func (c *failingRouterCommand) Description() string { return c.name }
func (c *failingRouterCommand) Execute(context.Context, *domain.CommandContext, map[string]any) error {
	return c.err
}
