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
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
	templateview "github.com/kapu/hololive-shared/pkg/templateview"
	"github.com/kapu/hololive-shared/pkg/util"
)

// llmSchedulerFormatter는 llm-scheduler가 사용하는 최소 메시지 포맷터 구현이다.
// bot 전용 adapter에 의존하지 않고 template.Renderer만으로 필요한 formatter 계약만 맞춘다.
type llmSchedulerFormatter struct {
	prefix   string
	renderer *template.Renderer
	logger   *slog.Logger
}

func newLLMSchedulerFormatter(prefix string, renderer *template.Renderer, logger *slog.Logger) *llmSchedulerFormatter {
	if stringutil.TrimSpace(prefix) == "" {
		prefix = "!"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &llmSchedulerFormatter{
		prefix:   prefix,
		renderer: renderer,
		logger:   logger,
	}
}

// UIEmoji/DefaultEmoji는 기존 템플릿 데이터(Emoji.* 참조) 호환을 위해 local 정의한다.
type UIEmoji struct {
	Brand     string
	Alarm     string
	Broadcast string
	Success   string
	Error     string
	Schedule  string
	Live      string
	Hint      string
	Time      string
	Info      string
	Member    string
	Link      string
	Web       string
	Speech    string
	Highlight string
	Data      string
	Stats     string
	Video     string
}

var DefaultEmoji = UIEmoji{
	Brand:     "🌸",
	Alarm:     "🔔",
	Broadcast: "📺",
	Success:   "✅",
	Error:     "❌",
	Schedule:  "📅",
	Live:      "🔴",
	Hint:      "💡",
	Time:      "⏰",
	Info:      "ℹ️",
	Member:    "📘",
	Link:      "🔗",
	Web:       "🌐",
	Speech:    "🗣️",
	Highlight: "✨",
	Data:      "📋",
	Stats:     "📊",
	Video:     "🎬",
}

const (
	errDisplayMajorEventFailed = "행사 알림 정보를 표시할 수 없습니다."
	errDisplayMemberNewsFailed = "멤버 뉴스 정보를 표시할 수 없습니다."
)

func errorMessage(message string) string {
	return fmt.Sprintf("%s %s", DefaultEmoji.Error, message)
}

func (f *llmSchedulerFormatter) render(ctx context.Context, key domain.TemplateKey, data any) (string, error) {
	if f == nil || f.renderer == nil {
		return "", fmt.Errorf("template renderer not configured")
	}

	rendered, err := f.renderer.Render(ctx, key, "", data)
	if err != nil {
		return "", fmt.Errorf("render template %s: %w", key, err)
	}
	return strings.TrimRight(rendered, "\n"), nil
}

func splitTemplateInstruction(rendered string) (instruction string, body string) {
	return templateview.SplitTemplateInstruction(rendered)
}

type majorEventWeeklySummaryData struct {
	Emoji      UIEmoji
	Count      int
	Events     []majorEventView
	LLMSummary string
}

type majorEventMonthlySummaryData struct {
	Emoji      UIEmoji
	Count      int
	Events     []majorEventView
	LLMSummary string
}

type majorEventView = templateview.MajorEventView

func (f *llmSchedulerFormatter) FormatMajorEventWeeklySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) string {
	if len(events) == 0 {
		return ""
	}

	normalizedSummary := strings.TrimSpace(llmSummary)
	views := buildMajorEventViews(events)
	if normalizedSummary != "" {
		// LLM 요약이 있는 경우 템플릿의 기본 목록과 중복 노출을 방지합니다.
		views = nil
	}

	data := majorEventWeeklySummaryData{
		Emoji:      DefaultEmoji,
		Count:      len(events),
		Events:     views,
		LLMSummary: normalizedSummary,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventWeeklySummary, data)
	if err != nil {
		f.logger.Warn("major event weekly summary render failed", slog.Any("error", err))
		return errorMessage(errDisplayMajorEventFailed)
	}

	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}
	return util.ApplyKakaoSeeMorePadding(body, instruction)
}

func (f *llmSchedulerFormatter) FormatMajorEventMonthlySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) string {
	if len(events) == 0 {
		return ""
	}

	normalizedSummary := strings.TrimSpace(llmSummary)
	views := buildMajorEventViews(events)
	if normalizedSummary != "" {
		views = nil
	}

	data := majorEventMonthlySummaryData{
		Emoji:      DefaultEmoji,
		Count:      len(events),
		Events:     views,
		LLMSummary: normalizedSummary,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventMonthlySummary, data)
	if err != nil {
		f.logger.Warn("major event monthly summary render failed", slog.Any("error", err))
		return errorMessage(errDisplayMajorEventFailed)
	}

	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}
	return util.ApplyKakaoSeeMorePadding(body, instruction)
}

func buildMajorEventViews(events []domain.MajorEvent) []majorEventView {
	return templateview.BuildMajorEventViews(events)
}

func formatMajorEventDatesFromDB(start, end *time.Time) string {
	return templateview.FormatMajorEventDatesFromDB(start, end)
}

type memberNewsDigestTemplateData struct {
	Emoji       UIEmoji
	Headline    string
	TopItems    []membernews.SummaryItem
	MoreSummary string
	TotalCount  int
}

func (f *llmSchedulerFormatter) FormatMemberNewsDigest(ctx context.Context, digest *membernews.Digest) string {
	if digest == nil {
		return errorMessage(errDisplayMemberNewsFailed)
	}

	data := memberNewsDigestTemplateData{
		Emoji:       DefaultEmoji,
		Headline:    digest.Headline,
		TopItems:    localizeMemberNewsItems(digest.TopItems),
		MoreSummary: digest.MoreSummary,
		TotalCount:  digest.TotalCount,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMemberNewsDigest, data)
	if err != nil {
		f.logger.Warn("member news digest render failed", slog.Any("error", err))
		return errorMessage(errDisplayMemberNewsFailed)
	}

	return rendered
}

func localizeMemberNewsItems(items []membernews.SummaryItem) []membernews.SummaryItem {
	if len(items) == 0 {
		return items
	}

	localized := make([]membernews.SummaryItem, len(items))
	copy(localized, items)
	for i := range localized {
		localized[i].Category = memberNewsCategoryLabel(localized[i].Category)
	}

	return localized
}

func memberNewsCategoryLabel(raw string) string {
	return templateview.MemberNewsCategoryLabel(raw)
}
