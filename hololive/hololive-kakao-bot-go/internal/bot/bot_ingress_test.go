package bot

import (
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

func TestMessageIngressPrepare_SkipsSelfSender(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ingress := NewMessageIngress(
		adapter.NewMessageAdapter("!"),
		nil,
		logger,
		stringutil.Normalize("봇계정"),
	)

	sender := "봇계정"
	msg := &iris.Message{
		Msg:    "!help",
		Room:   "테스트방",
		Sender: &sender,
	}

	envelope, ok := ingress.Prepare(msg)
	if ok {
		t.Fatalf("expected self message to be skipped")
	}
	if envelope != nil {
		t.Fatalf("expected nil envelope when skipped")
	}
}

func TestMessageIngressPrepare_ParsesCommand(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ingress := NewMessageIngress(
		adapter.NewMessageAdapter("!"),
		nil,
		logger,
		"",
	)

	sender := "사용자"
	msg := &iris.Message{
		Msg:    "!help",
		Room:   "홀로라이브",
		Sender: &sender,
		JSON: &iris.MessageJSON{
			UserID: "user-1",
			ChatID: "chat-123",
		},
	}

	envelope, ok := ingress.Prepare(msg)
	if !ok {
		t.Fatalf("expected command to be accepted")
	}
	if envelope == nil {
		t.Fatalf("expected non-nil envelope")
	}
	if envelope.ChatID != "chat-123" {
		t.Fatalf("chat id = %q, want %q", envelope.ChatID, "chat-123")
	}
	if envelope.RoomName != "홀로라이브" {
		t.Fatalf("room name = %q, want %q", envelope.RoomName, "홀로라이브")
	}
	if envelope.UserID != "user-1" {
		t.Fatalf("user id = %q, want %q", envelope.UserID, "user-1")
	}
	if envelope.UserName != "사용자" {
		t.Fatalf("user name = %q, want %q", envelope.UserName, "사용자")
	}
	if envelope.CommandType != domain.CommandHelp.String() {
		t.Fatalf("command type = %q, want %q", envelope.CommandType, domain.CommandHelp.String())
	}
	if envelope.Parsed == nil || envelope.Parsed.Type != domain.CommandHelp {
		t.Fatalf("parsed type = %v, want %v", envelope.Parsed.Type, domain.CommandHelp)
	}
}
