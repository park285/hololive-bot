package messaging

import (
	"slices"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func (ma *MessageAdapter) tryMemberInfoCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isMemberInfoCommand(command) {
		return nil, false
	}

	query := strings.TrimSpace(strings.Join(args, " "))
	params := make(map[string]any)

	if query != "" {
		params["query"] = query
	}

	return &ParsedCommand{Type: botcommand.CommandMemberInfo, Params: params, RawMessage: raw}, true
}

var memberInfoCommandAliases = []string{"멤버", "member", "프로필", "profile", "정보", "info"}

func (ma *MessageAdapter) isMemberInfoCommand(cmd string) bool {
	return slices.Contains(memberInfoCommandAliases, cmd)
}
