package formatter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type helpTemplateData struct {
	Prefix string
}

type HelpContent struct {
	TextFallback string
}

func (f *ResponseFormatter) FormatHelpContent(ctx context.Context) (HelpContent, error) {
	data := helpTemplateData{Prefix: f.prefix}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdHelp, data)
	if err != nil {
		return HelpContent{}, fmt.Errorf("render: %w", err)
	}

	if strings.TrimSpace(rendered) == "" {
		return HelpContent{}, errors.New("help template rendered empty")
	}

	return HelpContent{
		TextFallback: f.foldSeeMore(rendered, true),
	}, nil
}

func (f *ResponseFormatter) FormatHelp(ctx context.Context) string {
	content, err := f.FormatHelpContent(ctx)
	if err != nil {
		return f.renderFailureText()
	}

	return content.TextFallback
}
