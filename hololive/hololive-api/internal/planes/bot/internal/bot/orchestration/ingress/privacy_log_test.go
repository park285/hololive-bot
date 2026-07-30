package ingress

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/park285/iris-client-go/webhook"
	"github.com/park285/shared-go/pkg/stringutil"
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

func requireMessage(t *testing.T, records []capturedLogRecord, message string) capturedLogRecord {
	t.Helper()

	for _, record := range records {
		if record.message == message {
			return record
		}
	}

	t.Fatalf("log message %q must be emitted, got records %#v", message, records)
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

func TestMessageIngressCommandReceivedLogKeepsEventWithoutContentOrNickname(t *testing.T) {
	t.Parallel()

	logger, snapshot := newCapturingLogger()
	ing := NewMessageIngress(messaging.NewMessageAdapter("!", ""), nil, logger, "")

	sender := "닉네임-" + privacySentinel
	message := &webhook.Message{
		Msg:    "!member 검색어-" + privacySentinel,
		Room:   "룸이름-" + privacySentinel,
		Sender: &sender,
		JSON: &webhook.MessageJSON{
			UserID: "user-1",
			ChatID: "chat-123",
		},
	}

	envelope, ok := ing.Prepare(t.Context(), message)
	if !ok || envelope == nil {
		t.Fatal("expected command to be accepted")
	}

	records := snapshot()
	record := requireEvent(t, records, EventCommandReceived)
	if record.attrs["room_id"] != "chat-123" {
		t.Fatalf("room_id = %v, want %q", record.attrs["room_id"], "chat-123")
	}
	if record.attrs["user_id"] != "user-1" {
		t.Fatalf("user_id = %v, want %q", record.attrs["user_id"], "user-1")
	}

	assertNoLogSubstring(t, records, privacySentinel)
	assertNoLogAttrKey(t, records, "message_sha256_8")
	assertNoLogAttrKey(t, records, "user_name")
	assertNoLogAttrKey(t, records, "room_name")
}

func TestMessageIngressUnknownCommandLogKeepsEventWithoutContentOrNickname(t *testing.T) {
	t.Parallel()

	logger, snapshot := newCapturingLogger()
	ing := NewMessageIngress(messaging.NewMessageAdapter("!", ""), nil, logger, "")

	sender := "닉네임-" + privacySentinel
	message := &webhook.Message{
		Msg:    "!없는명령-" + privacySentinel,
		Room:   "룸이름-" + privacySentinel,
		Sender: &sender,
		JSON: &webhook.MessageJSON{
			UserID: "user-1",
			ChatID: "chat-123",
		},
	}

	if _, ok := ing.Prepare(t.Context(), message); ok {
		t.Fatal("expected unknown command to be rejected")
	}

	records := snapshot()
	record := requireMessage(t, records, "Unknown command ignored")
	if record.attrs["room_id"] != "chat-123" {
		t.Fatalf("room_id = %v, want %q", record.attrs["room_id"], "chat-123")
	}

	assertNoLogSubstring(t, records, privacySentinel)
	assertNoLogAttrKey(t, records, "message_sha256_8")
	assertNoLogAttrKey(t, records, "user_name")
}

func TestMessageIngressSelfSenderLogOmitsNickname(t *testing.T) {
	t.Parallel()

	selfSender := "봇계정-" + privacySentinel
	logger, snapshot := newCapturingLogger()
	ing := NewMessageIngress(messaging.NewMessageAdapter("!", ""), nil, logger, stringutil.Normalize(selfSender))

	message := &webhook.Message{
		Msg:    "!member 검색어-" + privacySentinel,
		Room:   "룸이름-" + privacySentinel,
		Sender: &selfSender,
		JSON: &webhook.MessageJSON{
			UserID: "user-1",
			ChatID: "chat-123",
		},
	}

	if _, ok := ing.Prepare(t.Context(), message); ok {
		t.Fatal("expected self-issued message to be skipped")
	}

	records := snapshot()
	record := requireMessage(t, records, "Skipping self-issued message")
	if record.attrs["room_id"] != "chat-123" {
		t.Fatalf("room_id = %v, want %q", record.attrs["room_id"], "chat-123")
	}

	assertNoLogSubstring(t, records, privacySentinel)
	assertNoLogAttrKey(t, records, "user")
	assertNoLogAttrKey(t, records, "user_name")
}
