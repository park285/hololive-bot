package messaging

import (
	"slices"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func (ma *MessageAdapter) tryHelpCommand(command, raw string) (*ParsedCommand, bool) {
	if !ma.isHelpCommand(command) {
		return nil, false
	}

	return &ParsedCommand{Type: botcommand.CommandHelp, Params: botcommand.NoParams{}, RawMessage: raw}, true
}

var helpCommandAliases = []string{"도움말", "도움", "help", "명령어", "commands"}

func (ma *MessageAdapter) isHelpCommand(cmd string) bool {
	return slices.Contains(helpCommandAliases, cmd)
}
