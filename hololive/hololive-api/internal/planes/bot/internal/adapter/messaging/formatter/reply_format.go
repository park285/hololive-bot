package formatter

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type replyFormatContextKey struct{}

func WithReplyFormat(ctx context.Context, format domain.TemplateFormat) context.Context {
	return context.WithValue(ctx, replyFormatContextKey{}, format)
}

func ReplyFormatFromContext(ctx context.Context) (domain.TemplateFormat, bool) {
	format, ok := ctx.Value(replyFormatContextKey{}).(domain.TemplateFormat)

	return format, ok
}

func replyFormat(ctx context.Context) domain.TemplateFormat {
	if format, ok := ReplyFormatFromContext(ctx); ok {
		return format
	}

	return domain.TemplateFormatPlain
}
