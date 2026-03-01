package adapter

import (
	"strings"
	"unicode"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
)

// MessageAdapter: 카카오톡 메시지를 커맨드로 파싱하는 어댑터입니다.
type MessageAdapter struct {
	prefix  string
	parsers []CommandParser
}

const (
	actionStatus = "status" // 공통: 구독 상태 조회

	memberNewsActionStatus = actionStatus
	memberNewsActionOn     = "on"
	memberNewsActionOff    = "off"
)

var legacyMQPrefixes = []string{"!", "/", "！"}

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

func (ma *MessageAdapter) extractCommandText(raw string) (normalized string, commandText string, ok bool) {
	text := trimLegacyLeading(raw)
	text = stringutil.TrimSpace(text)
	if text == "" {
		return text, "", false
	}

	prefixes := []string{normalizeCommandPrefix(ma.prefix)}
	for _, p := range legacyMQPrefixes {
		if p != prefixes[0] {
			prefixes = append(prefixes, p)
		}
	}

	for _, p := range prefixes {
		if !strings.HasPrefix(text, p) {
			continue
		}
		cmd := stringutil.TrimSpace(text[len(p):])
		if cmd == "" {
			return text, "", false
		}
		return text, cmd, true
	}

	return text, "", false
}

// NewMessageAdapter: MessageAdapter 인스턴스를 생성합니다.
func NewMessageAdapter(prefix string) *MessageAdapter {
	adapter := &MessageAdapter{
		prefix: normalizeCommandPrefix(prefix),
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
