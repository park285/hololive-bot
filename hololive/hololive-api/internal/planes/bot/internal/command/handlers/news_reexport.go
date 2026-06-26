package handlers

import "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/news"

type MemberNewsCommand = news.MemberNewsCommand
type MemberNewsSubscriptionCommand = news.MemberNewsSubscriptionCommand

func NewMemberNewsCommand(deps *Dependencies) *MemberNewsCommand {
	return news.NewMemberNewsCommand(deps)
}

func NewMemberNewsSubscriptionCommand(deps *Dependencies) *MemberNewsSubscriptionCommand {
	return news.NewMemberNewsSubscriptionCommand(deps)
}
