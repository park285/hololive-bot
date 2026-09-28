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
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	templateview "github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
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

func (f *llmSchedulerFormatter) render(ctx context.Context, key domain.TemplateKey, data any) (string, error) {
	if f == nil || f.renderer == nil {
		return "", errors.New("template renderer not configured")
	}

	rendered, err := f.renderer.Render(ctx, key, "", data)
	if err != nil {
		return "", fmt.Errorf("render template %s: %w", key, err)
	}

	return strings.TrimRight(rendered, "\n"), nil
}

// renderNotification은 예약 알림 본문을 렌더한다. 렌더에 실패하면 코드 대체 문구를 구독 방에 보내지 않고
// 오류를 돌려준다. 호출자는 enqueue하지 않고 알림을 미표시로 남겨 다음 주기에 다시 시도한다
// (DEC-20260926-hololive-message-strings-startup-validation).
func (f *llmSchedulerFormatter) renderNotification(ctx context.Context, key domain.TemplateKey, data any, failureMsg string) (string, error) {
	rendered, err := f.render(ctx, key, data)
	if err != nil {
		f.logger.Warn(failureMsg, slog.Any("error", err))

		return "", fmt.Errorf("%s: %w", failureMsg, err)
	}

	if f.seeMoreFold {
		return util.FoldForSeeMore(rendered, util.KakaoSeeMoreThreshold), nil
	}

	return rendered, nil
}

type majorEventSummaryData struct {
	Count      int
	Events     []templateview.MajorEventView
	LLMSummary string
}

func (f *llmSchedulerFormatter) FormatMajorEventWeeklySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) (string, error) {
	return f.formatMajorEventSummary(ctx, domain.TemplateKeyCmdMajorEventWeeklySummary, events, llmSummary)
}

func (f *llmSchedulerFormatter) FormatMajorEventMonthlySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) (string, error) {
	return f.formatMajorEventSummary(ctx, domain.TemplateKeyCmdMajorEventMonthlySummary, events, llmSummary)
}

func (f *llmSchedulerFormatter) formatMajorEventSummary(ctx context.Context, key domain.TemplateKey, events []domain.MajorEvent, llmSummary string) (string, error) {
	if len(events) == 0 {
		return "", nil
	}

	normalizedSummary := strings.TrimSpace(llmSummary)
	views := buildMajorEventViews(events)

	if normalizedSummary != "" {
		// LLM 요약이 있는 경우 템플릿의 기본 목록과 중복 노출을 방지합니다.
		views = nil
	}

	data := majorEventSummaryData{
		Count:      len(events),
		Events:     views,
		LLMSummary: normalizedSummary,
	}

	return f.renderNotification(ctx, key, data, majorEventSummaryWarnMsg(key))
}

func majorEventSummaryWarnMsg(key domain.TemplateKey) string {
	if key == domain.TemplateKeyCmdMajorEventMonthlySummary {
		return "major event monthly summary render failed"
	}

	return "major event weekly summary render failed"
}

func buildMajorEventViews(events []domain.MajorEvent) []templateview.MajorEventView {
	return templateview.BuildMajorEventViews(events)
}

func formatMajorEventDatesFromDB(start, end *time.Time) string {
	return templateview.FormatMajorEventDatesFromDB(start, end)
}

type memberNewsDigestTemplateData struct {
	Headline    string
	TopItems    []model.SummaryItem
	MoreSummary string
	TotalCount  int
}

func (f *llmSchedulerFormatter) FormatMemberNewsDigest(ctx context.Context, digest *model.Digest) (string, error) {
	if digest == nil {
		return "", errors.New("format member news digest: digest is nil")
	}

	data := memberNewsDigestTemplateData{
		Headline:    digest.Headline,
		TopItems:    f.localizeMemberNewsItems(ctx, digest.TopItems),
		MoreSummary: digest.MoreSummary,
		TotalCount:  digest.TotalCount,
	}

	return f.renderNotification(ctx, domain.TemplateKeyCmdMemberNewsDigest, data, "member news digest render failed")
}

func (f *llmSchedulerFormatter) localizeMemberNewsItems(ctx context.Context, items []model.SummaryItem) []model.SummaryItem {
	if len(items) == 0 {
		return items
	}

	localized := make([]model.SummaryItem, len(items))
	copy(localized, items)

	for i := range localized {
		localized[i].Category = f.memberNewsCategoryLabel(ctx, localized[i].Category)
	}

	return localized
}

func (f *llmSchedulerFormatter) memberNewsCategoryLabel(_ context.Context, raw string) string {
	if label, ok := f.store.Lookup(messagestrings.NamespaceNewsCat, strings.ToLower(strings.TrimSpace(raw))); ok {
		return label
	}

	return raw
}
