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
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
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
	for index := len(groups) - 1; index >= 0; index-- {
		attr = slog.Attr{Key: groups[index], Value: slog.GroupValue(attr)}
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

func newPrivacyCommandContext() *domain.CommandContext {
	threadID := "thread-1"
	return &domain.CommandContext{
		Room:     "123456789",
		RoomName: "룸이름-" + privacySentinel,
		UserID:   "user-1",
		UserName: "닉네임-" + privacySentinel,
		Message:  "!help 본문-" + privacySentinel,
		ThreadID: &threadID,
	}
}

func assertPrivacyAttrs(t *testing.T, record capturedLogRecord) {
	t.Helper()

	if record.attrs["room_id"] != "123456789" {
		t.Fatalf("room_id = %v, want %q", record.attrs["room_id"], "123456789")
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

	assertNoSentinelInLogs(t, records)
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

	assertNoSentinelInLogs(t, records)
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

func TestCommandContextAttrsKeepTheIngressRoomToken(t *testing.T) {
	t.Parallel()

	const roomName = "상대방닉네임 님과의 대화"

	cmdCtx := domain.NewCommandContext("", roomName, "user-1", "닉네임", "!알람", false)
	attrs := commandContextAttrs(cmdCtx, "alarm")

	var roomToken string
	for _, attr := range attrs {
		if attr.Key == privacylog.KeyRoomID {
			roomToken = attr.Value.String()
		}
	}

	if roomToken == privacylog.UnknownToken {
		t.Fatal("chat_id가 빈 경로에서 ingress와 상관 키가 갈렸다")
	}
	if roomToken != privacylog.RoomAttr("", roomName).Value.String() {
		t.Fatalf("room token = %q, want the ingress token %q",
			roomToken, privacylog.RoomAttr("", roomName).Value.String())
	}
	if strings.Contains(roomToken, roomName) {
		t.Fatalf("방 제목이 평문으로 남았다: %q", roomToken)
	}
}
