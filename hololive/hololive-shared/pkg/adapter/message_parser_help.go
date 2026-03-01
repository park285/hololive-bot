package adapter

import (
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (ma *MessageAdapter) tryHelpCommand(command string, raw string) (*ParsedCommand, bool) {
	if !ma.isHelpCommand(command) {
		return nil, false
	}
	return &ParsedCommand{Type: domain.CommandHelp, Params: make(map[string]any), RawMessage: raw}, true
}

func (ma *MessageAdapter) isHelpCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"도움말", "도움", "help", "명령어", "commands"}, cmd)
}
