package messaging

import (
	"strings"
	"unicode"

	"github.com/park285/iris-client-go/v6/webhook"
	"github.com/park285/shared-go/core/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

type MessageAdapter struct {
	prefix  string
	parsers []CommandParser
}

const (
	paramMember = "member"
	paramType   = "type"
	paramDays   = "days"
	paramTopic  = "topic"
	paramLimit  = "limit"
)

func normalizeCommandPrefix(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return "!"
	}

	return trimmed
}

func trimLeadingSpaceAndZeroWidth(text string) string {
	return strings.TrimLeftFunc(text, func(r rune) bool {
		if unicode.IsSpace(r) {
			return true
		}

		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff':
			return true
		default:
			return false
		}
	})
}

func (ma *MessageAdapter) extractCommandText(raw string) (normalized, commandText string, ok bool) {
	text := trimLeadingSpaceAndZeroWidth(raw)

	text = strings.TrimSpace(text)
	if text == "" {
		return text, "", false
	}

	prefix := normalizeCommandPrefix(ma.prefix)
	if !strings.HasPrefix(text, prefix) {
		return text, "", false
	}

	cmd := strings.TrimSpace(text[len(prefix):])
	if cmd == "" {
		return text, "", false
	}

	return text, cmd, true
}

func NewMessageAdapter(prefix string) *MessageAdapter {
	adapter := &MessageAdapter{
		prefix: normalizeCommandPrefix(prefix),
	}

	adapter.parsers = defaultCommandParsers(adapter)

	return adapter
}

type ParsedCommand struct {
	Type       botcommand.CommandType
	Params     botcommand.Params
	RawMessage string
}

func (ma *MessageAdapter) ParseMessage(message *webhook.Message) *ParsedCommand {
	text, commandText, ok := ma.commandTextFromMessage(message)
	if !ok {
		return ma.createUnknownCommand(text)
	}

	command, args, ok := parseCommandParts(commandText)
	if !ok {
		return ma.createUnknownCommand(text)
	}

	if normalizedCmd, normalizedArgs, ok := normalizeCompactAlarmTokens(command, args); ok {
		command = normalizedCmd
		args = normalizedArgs
	}

	if parsed, ok := ma.parseKnownCommand(command, args, text); ok {
		return parsed
	}

	return ma.createUnknownCommand(text)
}

func (ma *MessageAdapter) commandTextFromMessage(message *webhook.Message) (text, commandText string, ok bool) {
	if message == nil || message.Msg == "" {
		return "", "", false
	}

	return ma.extractCommandText(message.Msg)
}

func parseCommandParts(commandText string) (command string, args []string, ok bool) {
	parts := strings.Fields(commandText)
	if len(parts) == 0 {
		return "", nil, false
	}

	return stringutil.Normalize(parts[0]), parts[1:], true
}

func (ma *MessageAdapter) parseKnownCommand(command string, args []string, text string) (*ParsedCommand, bool) {
	for _, parser := range ma.parsers {
		if parser == nil {
			continue
		}

		if parsed, ok := parser.Parse(command, args, text); ok {
			return parsed, true
		}
	}

	return nil, false
}

func (ma *MessageAdapter) createUnknownCommand(text string) *ParsedCommand {
	return &ParsedCommand{
		Type:       botcommand.CommandUnknown,
		RawMessage: text,
	}
}
