// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package messaging

import (
	"strings"
	"unicode"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/shared-go/pkg/stringutil"
)

type MessageAdapter struct {
	prefix        string
	mentionPrefix string
	parsers       []CommandParser
}

const (
	actionStatus = "status" // 공통: 구독 상태 조회

	memberNewsActionStatus = actionStatus
	memberNewsActionOn     = "on"
	memberNewsActionOff    = "off"
)

func normalizeCommandPrefix(prefix string) string {
	trimmed := stringutil.TrimSpace(prefix)
	if trimmed == "" {
		return "!"
	}

	return trimmed
}

func trimLegacyLeading(text string) string {
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
	text := trimLegacyLeading(raw)

	text = stringutil.TrimSpace(text)
	if text == "" {
		return text, "", false
	}

	prefix := normalizeCommandPrefix(ma.prefix)
	if !strings.HasPrefix(text, prefix) {
		return text, "", false
	}

	cmd := stringutil.TrimSpace(text[len(prefix):])
	if cmd == "" {
		return text, "", false
	}

	return text, cmd, true
}

func NewMessageAdapter(prefix string, mentionPrefix string) *MessageAdapter {
	adapter := &MessageAdapter{
		prefix:        normalizeCommandPrefix(prefix),
		mentionPrefix: mentionPrefix,
	}

	adapter.parsers = defaultCommandParsers(adapter)

	return adapter
}

type ParsedCommand struct {
	Type       domain.CommandType
	Params     map[string]any
	RawMessage string
}

func (ma *MessageAdapter) ParseMessage(message *iris.Message) *ParsedCommand {
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

func (ma *MessageAdapter) commandTextFromMessage(message *iris.Message) (string, string, bool) {
	if message == nil || message.Msg == "" {
		return "", "", false
	}

	return ma.extractCommandText(message.Msg)
}

func parseCommandParts(commandText string) (string, []string, bool) {
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
		Type:       domain.CommandUnknown,
		Params:     make(map[string]any),
		RawMessage: text,
	}
}
