package ingress

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
	"github.com/park285/iris-client-go/v2/webhook"
	"github.com/park285/shared-go/v2/pkg/stringutil"
)

const privacySentinel = "SENTINEL"

type capturedLogRecord struct {
	message string
	attrs   map[string]any
}

type capturingHandler struct {
	mu      *sync.Mutex
	records *[]capturedLogRecord
	attrs   []slog.Attr
	groups  []string
}

func (h capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h capturingHandler) Handle(_ context.Context, record slog.Record) error { //nolint:gocritic // hugeParam: slog.Handler.Handle 인터페이스가 값 전달 시그니처를 강제
	captured := capturedLogRecord{message: record.Message, attrs: make(map[string]any, record.NumAttrs()+len(h.attrs))}
	for _, attr := range h.attrs {
		flattenLogAttr(captured.attrs, nil, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		flattenLogAttr(captured.attrs, h.groups, attr)
		return true
	})

	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, captured)

	return nil
}

func (h capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	next := h
	next.attrs = make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	next.attrs = append(next.attrs, h.attrs...)
	for _, attr := range attrs {
		next.attrs = append(next.attrs, groupedLogAttr(h.groups, attr))
	}

	return next
}

func (h capturingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	next := h
	next.groups = appendLogGroup(h.groups, name)

	return next
}

func appendLogGroup(groups []string, name string) []string {
	return append(append(make([]string, 0, len(groups)+1), groups...), name)
}

func groupedLogAttr(groups []string, attr slog.Attr) slog.Attr {
	for _, group := range slices.Backward(groups) {
		attr = slog.Attr{Key: group, Value: slog.GroupValue(attr)}
	}

	return attr
}

func flattenLogAttr(dst map[string]any, groups []string, attr slog.Attr) {
	value := attr.Value.Resolve()
	if value.Kind() != slog.KindGroup {
		dst[logAttrKey(groups, attr.Key)] = value.Any()
		return
	}

	members := value.Group()
	if len(members) == 0 {
		return
	}

	nested := groups
	if attr.Key != "" {
		nested = appendLogGroup(groups, attr.Key)
	}
	for _, member := range members {
		flattenLogAttr(dst, nested, member)
	}
}

func logAttrKey(groups []string, key string) string {
	if len(groups) == 0 {
		return key
	}

	return strings.Join(appendLogGroup(groups, key), ".")
}

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

func assertNoSentinelInLogs(t *testing.T, records []capturedLogRecord) {
	t.Helper()

	for _, record := range records {
		if strings.Contains(record.message, privacySentinel) {
			t.Fatalf("log message leaked %q in record %#v", privacySentinel, record)
		}
		for key, value := range record.attrs {
			if strings.Contains(fmt.Sprint(value), privacySentinel) {
				t.Fatalf("log attr %s leaked %q in record %#v", key, privacySentinel, record)
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

func TestMessageIngressNeverLogsRoomTitleAsRoomID(t *testing.T) {
	t.Parallel()

	selfSender := "봇계정-" + privacySentinel
	otherSender := "다른사람-" + privacySentinel
	roomTitle := "룸제목-" + privacySentinel

	cases := []struct {
		name    string
		message *webhook.Message
		record  string
	}{
		{
			name:    "self-issued message without json envelope",
			message: &webhook.Message{Msg: "!member 검색어", Room: roomTitle, Sender: &selfSender},
			record:  "Skipping self-issued message",
		},
		{
			name:    "unknown command without json envelope",
			message: &webhook.Message{Msg: "!없는명령", Room: roomTitle, Sender: &otherSender},
			record:  "Unknown command ignored",
		},
		{
			name: "unknown command with an omitted chat id",
			message: &webhook.Message{
				Msg:    "!없는명령",
				Room:   roomTitle,
				Sender: &otherSender,
				JSON:   &webhook.MessageJSON{UserID: "user-1"},
			},
			record: "Unknown command ignored",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logger, snapshot := newCapturingLogger()
			ing := NewMessageIngress(messaging.NewMessageAdapter("!", ""), nil, logger, stringutil.Normalize(selfSender))

			if _, ok := ing.Prepare(t.Context(), tc.message); ok {
				t.Fatal("expected the message to be rejected")
			}

			records := snapshot()
			record := requireMessage(t, records, tc.record)

			roomID, ok := record.attrs[privacylog.KeyRoomID].(string)
			if !ok {
				t.Fatalf("room_id attr = %#v, want a string", record.attrs[privacylog.KeyRoomID])
			}
			if !strings.HasPrefix(roomID, privacylog.PseudonymPrefix) {
				t.Fatalf("room_id = %q, want the %q pseudonym of a non-canonical room", roomID, privacylog.PseudonymPrefix)
			}
			if roomID != privacylog.Pseudonym(roomTitle) {
				t.Fatalf("room_id = %q, want the deterministic token %q", roomID, privacylog.Pseudonym(roomTitle))
			}

			assertNoSentinelInLogs(t, records)
		})
	}
}

func TestMessageIngressKeepsCanonicalRoomIDReadable(t *testing.T) {
	t.Parallel()

	sender := "닉네임-" + privacySentinel
	logger, snapshot := newCapturingLogger()
	ing := NewMessageIngress(messaging.NewMessageAdapter("!", ""), nil, logger, "")

	message := &webhook.Message{
		Msg:    "!없는명령",
		Room:   "18446744073709551615",
		Sender: &sender,
	}

	if _, ok := ing.Prepare(t.Context(), message); ok {
		t.Fatal("expected unknown command to be rejected")
	}

	records := snapshot()
	record := requireMessage(t, records, "Unknown command ignored")
	if record.attrs[privacylog.KeyRoomID] != "18446744073709551615" {
		t.Fatalf("room_id = %v, want the canonical numeric room id", record.attrs[privacylog.KeyRoomID])
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
			ChatID: "123456789",
		},
	}

	envelope, ok := ing.Prepare(t.Context(), message)
	if !ok || envelope == nil {
		t.Fatal("expected command to be accepted")
	}

	records := snapshot()
	record := requireEvent(t, records, EventCommandReceived)
	if record.attrs["room_id"] != "123456789" {
		t.Fatalf("room_id = %v, want %q", record.attrs["room_id"], "123456789")
	}
	if record.attrs["user_id"] != "user-1" {
		t.Fatalf("user_id = %v, want %q", record.attrs["user_id"], "user-1")
	}

	assertNoSentinelInLogs(t, records)
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
			ChatID: "123456789",
		},
	}

	if _, ok := ing.Prepare(t.Context(), message); ok {
		t.Fatal("expected unknown command to be rejected")
	}

	records := snapshot()
	record := requireMessage(t, records, "Unknown command ignored")
	if record.attrs["room_id"] != "123456789" {
		t.Fatalf("room_id = %v, want %q", record.attrs["room_id"], "123456789")
	}

	assertNoSentinelInLogs(t, records)
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
			ChatID: "123456789",
		},
	}

	if _, ok := ing.Prepare(t.Context(), message); ok {
		t.Fatal("expected self-issued message to be skipped")
	}

	records := snapshot()
	record := requireMessage(t, records, "Skipping self-issued message")
	if record.attrs["room_id"] != "123456789" {
		t.Fatalf("room_id = %v, want %q", record.attrs["room_id"], "123456789")
	}

	assertNoSentinelInLogs(t, records)
	assertNoLogAttrKey(t, records, "user")
	assertNoLogAttrKey(t, records, "user_name")
}

func TestRoomLogAttrMatchesTheSharedCorrelationDefinition(t *testing.T) {
	t.Parallel()

	cases := []struct{ chatID, roomName string }{
		{"", "상대방닉네임 님과의 대화"},
		{"   ", "상대방닉네임 님과의 대화"},
		{"18446744073709551615", "상대방닉네임 님과의 대화"},
		{"", ""},
	}

	for _, tc := range cases {
		got := roomLogAttr(tc.chatID, tc.roomName)
		want := privacylog.RoomAttr(tc.chatID, tc.roomName)
		if got.Key != want.Key || got.Value.String() != want.Value.String() {
			t.Fatalf("roomLogAttr(%q, %q) = %v, want %v", tc.chatID, tc.roomName, got, want)
		}
	}
}
