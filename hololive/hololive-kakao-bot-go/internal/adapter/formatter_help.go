package adapter

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

type helpTemplateData struct {
	Emoji  UIEmoji
	Prefix string
}

func (f *ResponseFormatter) FormatHelp(ctx context.Context) string {
	data := helpTemplateData{Emoji: DefaultEmoji, Prefix: f.prefix}
	rendered, err := f.render(ctx, domain.TemplateKeyCmdHelp, data)
	if err != nil {
		return ErrorMessage(ErrDisplayHelpFailed)
	}
	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}
	return util.ApplyKakaoSeeMorePadding(body, instruction)
}
