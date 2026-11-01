package formatter

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type replyFormatContextKey struct{}

// WithReplyFormat은 이 요청 응답의 본문 변형을 ctx에 고정한다. 카카오 일반채팅은 RichText 서식을 표시하지 않으므로
// 방 유형 판정은 요청마다 한 번만 하고, formatter의 템플릿 변형 선택과 transport의 발송 경로 선택이 같은 값을 쓴다.
func WithReplyFormat(ctx context.Context, format domain.TemplateFormat) context.Context {
	return context.WithValue(ctx, replyFormatContextKey{}, format)
}

// ReplyFormatFromContext는 고정된 변형을 돌려준다. 판정을 고정하지 않은 경로면 두 번째 값이 false다.
func ReplyFormatFromContext(ctx context.Context) (domain.TemplateFormat, bool) {
	format, ok := ctx.Value(replyFormatContextKey{}).(domain.TemplateFormat)

	return format, ok
}

// replyFormat은 판정을 고정하지 않은 경로에서 일반 텍스트 변형을 쓴다. 일반 텍스트 본문은 어느 방에서도 읽을 수 있다.
func replyFormat(ctx context.Context) domain.TemplateFormat {
	if format, ok := ReplyFormatFromContext(ctx); ok {
		return format
	}

	return domain.TemplateFormatPlain
}
