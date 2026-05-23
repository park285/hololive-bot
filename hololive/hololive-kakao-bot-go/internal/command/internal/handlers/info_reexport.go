package handlers

import "github.com/kapu/hololive-kakao-bot-go/internal/command/internal/handlers/internal/info"

type MemberInfoCommand = info.MemberInfoCommand

func NewMemberInfoCommand(deps *Dependencies) *MemberInfoCommand {
	return info.NewMemberInfoCommand(deps)
}
