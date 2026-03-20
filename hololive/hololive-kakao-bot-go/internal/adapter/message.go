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

package adapter

import (
	"strings"
	"unicode"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// MessageAdapter: 카카오톡 메시지를 커맨드로 파싱하는 어댑터입니다.
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

// NewMessageAdapter: MessageAdapter 인스턴스를 생성합니다.
func NewMessageAdapter(prefix string, mentionPrefix string) *MessageAdapter {
	adapter := &MessageAdapter{
		prefix:        normalizeCommandPrefix(prefix),
		mentionPrefix: mentionPrefix,
	}

	adapter.parsers = defaultCommandParsers(adapter)

	return adapter
}

// ParsedCommand: 파싱된 커맨드 정보를 담는 구조체입니다.
type ParsedCommand struct {
	Type       domain.CommandType
	Params     map[string]any
	RawMessage string
}

// ParseMessage: 입력 메시지를 분석하여 커맨드 유형과 파라미터를 추출합니다.
func (ma *MessageAdapter) ParseMessage(message *iris.Message) *ParsedCommand {
	if message == nil || message.Msg == "" {
		return ma.createUnknownCommand("")
	}

	text, commandText, ok := ma.extractCommandText(message.Msg)
	if !ok {
		return ma.createUnknownCommand(text)
	}

	parts := strings.Fields(commandText)
	if len(parts) == 0 {
		return ma.createUnknownCommand(text)
	}

	command := stringutil.Normalize(parts[0])
	args := parts[1:]

	if normalizedCmd, normalizedArgs, ok := normalizeCompactAlarmTokens(command, args); ok {
		command = normalizedCmd
		args = normalizedArgs
	}

	for _, parser := range ma.parsers {
		if parser == nil {
			continue
		}

		if parsed, ok := parser.Parse(command, args, text); ok {
			return parsed
		}
	}

	return ma.createUnknownCommand(text)
}

func (ma *MessageAdapter) createUnknownCommand(text string) *ParsedCommand {
	return &ParsedCommand{
		Type:       domain.CommandUnknown,
		Params:     make(map[string]any),
		RawMessage: text,
	}
}

