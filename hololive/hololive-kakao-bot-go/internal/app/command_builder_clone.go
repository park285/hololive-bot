package app

import "github.com/kapu/hololive-kakao-bot-go/internal/bot"

func cloneCommandBuilders(src []bot.CommandBuilder) []bot.CommandBuilder {
	if src == nil {
		return nil
	}

	dst := make([]bot.CommandBuilder, len(src))
	copy(dst, src)

	return dst
}
