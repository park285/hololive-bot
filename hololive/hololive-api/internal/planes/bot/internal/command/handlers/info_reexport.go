package handlers

import "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/info"

type MemberInfoCommand = info.MemberInfoCommand

func NewMemberInfoCommand(deps *Dependencies) *MemberInfoCommand {
	return info.NewMemberInfoCommand(deps)
}
