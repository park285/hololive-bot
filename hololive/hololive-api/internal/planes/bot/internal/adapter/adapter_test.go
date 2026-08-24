package adapter_test

import (
	"testing"

	"github.com/park285/iris-client-go/v2/webhook"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestMessageAdapterParsesHelpCommand(t *testing.T) {
	messageAdapter := messaging.NewMessageAdapter("!", "")

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
	messageAdapter := messaging.NewMessageAdapter("!", "")

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
	messageAdapter := messaging.NewMessageAdapter("   ", "")

	parsed := messageAdapter.ParseMessage(&webhook.Message{Msg: "!help"})
	if parsed.Type != domain.CommandHelp {
		t.Fatalf("Type = %q, want CommandHelp via default prefix", parsed.Type)
	}
}
