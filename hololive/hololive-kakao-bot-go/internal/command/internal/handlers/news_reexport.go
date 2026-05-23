package handlers

import "github.com/kapu/hololive-kakao-bot-go/internal/command/internal/handlers/internal/news"

type MemberNewsCommand = news.MemberNewsCommand
type MemberNewsSubscriptionCommand = news.MemberNewsSubscriptionCommand

func NewMemberNewsCommand(deps *Dependencies) *MemberNewsCommand {
	return news.NewMemberNewsCommand(deps)
}

func NewMemberNewsSubscriptionCommand(deps *Dependencies) *MemberNewsSubscriptionCommand {
	return news.NewMemberNewsSubscriptionCommand(deps)
}
