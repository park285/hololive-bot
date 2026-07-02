package handlers

import "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/info"

type MemberInfoCommand = info.MemberInfoCommand

func NewMemberInfoCommand(deps *Dependencies, imageRenderer ProfileImageRenderer) *MemberInfoCommand {
	return info.NewMemberInfoCommand(deps, imageRenderer)
}
