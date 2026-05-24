package handlers

import "github.com/kapu/hololive-kakao-bot-go/internal/command/handlers/info"

type MemberInfoCommand = info.MemberInfoCommand

func NewMemberInfoCommand(deps *Dependencies) *MemberInfoCommand {
	return info.NewMemberInfoCommand(deps)
}
