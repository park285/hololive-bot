package adapter_test

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/webhook"

	adapter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
)

func TestMessageAdapterParsesHelpCommand(t *testing.T) {
	messageAdapter := adapter.NewMessageAdapter("!", "")

	for _, tc := range []struct {
		name    string
		msg     string
		wantRaw string
	}{
		{name: "korean alias", msg: "!도움말", wantRaw: "!도움말"},
		{name: "english alias", msg: "!help", wantRaw: "!help"},
		{name: "surrounding whitespace trimmed", msg: "  !help  ", wantRaw: "!help"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed := messageAdapter.ParseMessage(&webhook.Message{Msg: tc.msg})
			if parsed == nil {
				t.Fatal("ParseMessage returned nil")
			}
			if parsed.Type != domain.CommandHelp {
				t.Fatalf("Type = %q, want CommandHelp", parsed.Type)
			}
			if parsed.RawMessage != tc.wantRaw {
				t.Fatalf("RawMessage = %q, want %q", parsed.RawMessage, tc.wantRaw)
			}
			if parsed.Params == nil {
				t.Fatal("Params = nil, want non-nil map")
			}
		})
	}
}

func TestMessageAdapterReturnsUnknownForNonCommandInput(t *testing.T) {
	messageAdapter := adapter.NewMessageAdapter("!", "")

	for _, tc := range []struct {
		name    string
		message *webhook.Message
	}{
		{name: "missing prefix", message: &webhook.Message{Msg: "도움말"}},
		{name: "empty message", message: &webhook.Message{Msg: ""}},
		{name: "prefix only", message: &webhook.Message{Msg: "!"}},
		{name: "nil message", message: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed := messageAdapter.ParseMessage(tc.message)
			if parsed == nil {
				t.Fatal("ParseMessage returned nil")
			}
			if parsed.Type != domain.CommandUnknown {
				t.Fatalf("Type = %q, want CommandUnknown", parsed.Type)
			}
		})
	}
}

func TestMessageAdapterBlankPrefixDefaultsToExclamation(t *testing.T) {
	messageAdapter := adapter.NewMessageAdapter("   ", "")

	parsed := messageAdapter.ParseMessage(&webhook.Message{Msg: "!help"})
	if parsed.Type != domain.CommandHelp {
		t.Fatalf("Type = %q, want CommandHelp via default prefix", parsed.Type)
	}
}
