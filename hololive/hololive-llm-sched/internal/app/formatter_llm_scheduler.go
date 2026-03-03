package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
)

// llmSchedulerFormatter는 llm-scheduler가 사용하는 최소 메시지 포맷터 구현이다.
//
// NOTE:
// P9-1(adapter 이동) 대비: llm-sched는 bot 전용 adapter 구현에 의존하지 않는다.
// 대신, template.Renderer를 기반으로 majorevent/membernews scheduler가 요구하는 formatter 계약만 구현한다.
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
	trimmed := strings.TrimLeft(rendered, "\r\n")
	if trimmed == "" {
		return "", ""
	}

	parts := strings.SplitN(trimmed, "\n", 2)
	instruction = stringutil.TrimSpace(strings.TrimSuffix(parts[0], "\r"))
	if len(parts) < 2 {
		return instruction, ""
	}

	body = strings.TrimLeft(parts[1], "\r\n")
	return instruction, body
}

// --- MajorEvent formatter (majorevent.Formatter) ---

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

type majorEventView struct {
	Title    string
	DateStr  string
	Members  string
	Link     string
	HasDates bool
}

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
	views := make([]majorEventView, 0, len(events))
	for i := range events {
		event := &events[i]
		views = append(views, majorEventView{
			Title:    event.Title,
			DateStr:  formatMajorEventDatesFromDB(event.EventStartDate, event.EventEndDate),
			Members:  strings.Join(event.Members, ", "),
			Link:     event.Link,
			HasDates: event.EventStartDate != nil,
		})
	}
	return views
}

func formatMajorEventDatesFromDB(start, end *time.Time) string {
	if start == nil {
		return "TBA"
	}

	weekdays := []string{"일", "월", "화", "수", "목", "금", "토"}
	formatDate := func(t time.Time) string {
		return fmt.Sprintf("%d년 %d월 %d일(%s)", t.Year(), t.Month(), t.Day(), weekdays[t.Weekday()])
	}

	if end == nil || start.Equal(*end) {
		return formatDate(*start)
	}

	return fmt.Sprintf("%s ~ %s", formatDate(*start), formatDate(*end))
}

// --- MemberNews formatter (membernews.DigestFormatter) ---

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
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "birthday_live":
		return "생일 라이브"
	case "solo_live":
		return "솔로 라이브"
	case "collab":
		return "콜라보"
	case "event":
		return "이벤트"
	case "goods":
		return "굿즈"
	case "other":
		return "기타"
	default:
		return raw
	}
}
