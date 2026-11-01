// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/llm/membernews/model"
	templateview "github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// llmSchedulerFormatter는 llm-scheduler가 사용하는 최소 메시지 포맷터 구현이다.
// 이 구현은 bot 전용 adapter에 의존하지 않고 template.Renderer만으로 필요한 formatter 계약만 맞춘다.
type llmSchedulerFormatter struct {
	prefix      string
	renderer    *template.Renderer
	store       *messagestrings.Store
	logger      *slog.Logger
	seeMoreFold bool
}

func newLLMSchedulerFormatter(prefix string, renderer *template.Renderer, logger *slog.Logger, seeMoreFold bool) *llmSchedulerFormatter {
	if stringutil.TrimSpace(prefix) == "" {
		prefix = "!"
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &llmSchedulerFormatter{
		prefix:      prefix,
		renderer:    renderer,
		logger:      logger,
		seeMoreFold: seeMoreFold,
	}
}

func (f *llmSchedulerFormatter) render(ctx context.Context, key domain.TemplateKey, format domain.TemplateFormat, data any) (string, error) {
	if f == nil || f.renderer == nil {
		return "", errors.New("template renderer not configured")
	}

	rendered, err := f.renderer.Render(ctx, key, "", format, data)
	if err != nil {
		return "", fmt.Errorf("render %s template %s: %w", format, key, err)
	}

	return strings.TrimRight(rendered, "\n"), nil
}

// renderNotification은 예약 알림을 방 유형별 두 본문으로 렌더한다. 같은 메시지를 여러 방에 enqueue하고 발송기가
// 방마다 하나를 고르므로 두 변형이 모두 있어야 한다. 실패하면 호출자가 enqueue하지 않고 알림을 미표시로 남겨 다음
// 주기에 다시 시도한다.
func (f *llmSchedulerFormatter) renderNotification(ctx context.Context, key domain.TemplateKey, data any, failureMsg string, foldEligible bool) (delivery.MessageVariants, error) {
	var message delivery.MessageVariants

	plain, err := f.render(ctx, key, domain.TemplateFormatPlain, data)
	if err == nil {
		message.Plain = plain
		message.RichText, err = f.render(ctx, key, domain.TemplateFormatRichText, data)
	}

	if err == nil && (strings.TrimSpace(message.Plain) == "" || strings.TrimSpace(message.RichText) == "") {
		err = errors.New("template rendered empty")
	}

	if err != nil {
		f.logger.Warn(failureMsg, slog.Any("error", err))

		return delivery.MessageVariants{}, fmt.Errorf("%s: %w", failureMsg, err)
	}

	if f.seeMoreFold && foldEligible {
		message.Plain = templateview.FoldForSeeMore(message.Plain)
		message.RichText = templateview.FoldForSeeMore(message.RichText)
	}

	return message, nil
}

type majorEventSummaryData struct {
	Count      int
	Events     []templateview.MajorEventView
	LLMSummary string
}

func (f *llmSchedulerFormatter) FormatMajorEventWeeklySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) (delivery.MessageVariants, error) {
	return f.formatMajorEventSummary(ctx, domain.TemplateKeyCmdMajorEventWeeklySummary, events, llmSummary)
}

func (f *llmSchedulerFormatter) FormatMajorEventMonthlySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) (delivery.MessageVariants, error) {
	return f.formatMajorEventSummary(ctx, domain.TemplateKeyCmdMajorEventMonthlySummary, events, llmSummary)
}

func (f *llmSchedulerFormatter) formatMajorEventSummary(ctx context.Context, key domain.TemplateKey, events []domain.MajorEvent, llmSummary string) (delivery.MessageVariants, error) {
	if len(events) == 0 {
		return delivery.MessageVariants{}, nil
	}

	normalizedSummary := strings.TrimSpace(kakaotext.MarkdownLiteral(strings.TrimSpace(llmSummary)))

	var views []templateview.MajorEventView

	if normalizedSummary == "" {
		// 표시할 요약이 없을 때만 기본 목록을 만들며, 외부 ZWSP만 있는 요약도 빈 값으로 취급한다.
		views = templateview.BuildMajorEventViews(events)
	}

	data := majorEventSummaryData{
		Count:      len(events),
		Events:     views,
		LLMSummary: normalizedSummary,
	}

	return f.renderNotification(ctx, key, data, majorEventSummaryWarnMsg(key), templateview.ShouldFoldItems(len(events)))
}

func majorEventSummaryWarnMsg(key domain.TemplateKey) string {
	if key == domain.TemplateKeyCmdMajorEventMonthlySummary {
		return "major event monthly summary render failed"
	}

	return "major event weekly summary render failed"
}

func (f *llmSchedulerFormatter) FormatMemberNewsDigest(ctx context.Context, digest *model.Digest) (delivery.MessageVariants, error) {
	if digest == nil {
		return delivery.MessageVariants{}, errors.New("format member news digest: digest is nil")
	}

	data := templateview.BuildMemberNewsDigest(*digest, f.store)

	return f.renderNotification(ctx, domain.TemplateKeyCmdMemberNewsDigest, data, "member news digest render failed", templateview.ShouldFoldItems(data.DisplayCount))
}
