package formatter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type ResponseFormatter struct {
	prefix         string
	renderer       *template.Renderer
	messageStrings *messagestrings.Store
	logger         *slog.Logger
	seeMoreFold    bool
}

type Option func(*ResponseFormatter)

func WithMessageStrings(store *messagestrings.Store) Option {
	return func(f *ResponseFormatter) { f.messageStrings = store }
}

func WithLogger(logger *slog.Logger) Option {
	return func(f *ResponseFormatter) { f.logger = logger }
}

func WithSeeMoreFold(enabled bool) Option {
	return func(f *ResponseFormatter) { f.seeMoreFold = enabled }
}

func (f *ResponseFormatter) foldSeeMore(s string, foldEligible bool) string {
	if f == nil || !f.seeMoreFold || !foldEligible {
		return s
	}

	return templateview.FoldForSeeMore(s)
}

func (f *ResponseFormatter) renderResponse(ctx context.Context, key domain.TemplateKey, data any, foldEligible bool) string {
	rendered, err := f.render(ctx, key, data)
	if err != nil {
		return f.renderFailureText()
	}

	return f.foldSeeMore(rendered, foldEligible)
}

func (f *ResponseFormatter) render(ctx context.Context, key domain.TemplateKey, data any) (string, error) {
	if f == nil || f.renderer == nil {
		return "", fmt.Errorf("render template %s: renderer not configured", key)
	}

	rendered, err := f.renderer.Render(ctx, key, "", replyFormat(ctx), data)
	if err != nil {
		if f.logger != nil {
			f.logger.ErrorContext(ctx, "template render failed", slog.String("template_key", string(key)), slog.Any("error", err))
		}

		return "", fmt.Errorf("render template %s: %w", key, err)
	}

	return strings.TrimRight(rendered, "\n"), nil
}

func NewResponseFormatter(prefix string, renderer *template.Renderer, opts ...Option) *ResponseFormatter {
	if strings.TrimSpace(prefix) == "" {
		prefix = "!"
	}

	f := &ResponseFormatter{prefix: prefix, renderer: renderer}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

func (f *ResponseFormatter) Prefix() string {
	if f == nil {
		return "!"
	}

	if trimmed := strings.TrimSpace(f.prefix); trimmed != "" {
		return trimmed
	}

	return "!"
}

func (f *ResponseFormatter) ResolveError(_ context.Context, key string) string {
	if f == nil {
		return ""
	}

	return f.messageStrings.Text(messagestrings.Key{Namespace: messagestrings.NamespaceError, Name: key})
}

func (f *ResponseFormatter) renderFailureText() string {
	if f == nil {
		return ""
	}

	return f.messageStrings.Text(RenderFailureMessageKey)
}

func (f *ResponseFormatter) GraduatedMemberWarning(ctx context.Context) string {
	if f == nil {
		return ""
	}

	text := f.messageStrings.Text(messagestrings.NotifyGraduatedMemberWarning)
	warning := strings.TrimRight(text, "\r\n")

	if warning == "" {
		return text
	}

	literal := kakaoformat.EscapeMarkdown(warning)

	if replyFormat(ctx) == domain.TemplateFormatRichText {
		literal = "**" + literal + "**"
	}

	return literal + text[len(warning):]
}

type memberNotFoundTemplateData struct {
	MemberName string
}

func (f *ResponseFormatter) MemberNotFound(ctx context.Context, memberName string) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNotFound, memberNotFoundTemplateData{MemberName: memberName})
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}
