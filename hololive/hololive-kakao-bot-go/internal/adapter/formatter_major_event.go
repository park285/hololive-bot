package adapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

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

type majorEventSubscribedData struct {
	Emoji  UIEmoji
	Prefix string
}

type majorEventStatusData struct {
	Emoji        UIEmoji
	IsSubscribed bool
	Prefix       string
}

type majorEventUsageData struct {
	Emoji  UIEmoji
	Prefix string
}

func (f *ResponseFormatter) FormatMajorEventWeeklySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) string {
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
		return ErrorMessage(ErrDisplayMajorEventFailed)
	}

	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}
	return util.ApplyKakaoSeeMorePadding(body, instruction)
}

// FormatMajorEventMonthlySummary: 월간 행사 요약을 포맷합니다.
func (f *ResponseFormatter) FormatMajorEventMonthlySummary(ctx context.Context, events []domain.MajorEvent, llmSummary string) string {
	if len(events) == 0 {
		return ""
	}

	normalizedSummary := strings.TrimSpace(llmSummary)
	views := buildMajorEventViews(events)
	if normalizedSummary != "" {
		// LLM 요약이 있는 경우 템플릿의 기본 목록과 중복 노출을 방지합니다.
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
		return ErrorMessage(ErrDisplayMajorEventFailed)
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

func (f *ResponseFormatter) FormatMajorEventSubscribed(ctx context.Context) string {
	data := majorEventSubscribedData{
		Emoji:  DefaultEmoji,
		Prefix: f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventSubscribed, data)
	if err != nil {
		return ErrorMessage(ErrDisplayMajorEventFailed)
	}
	return rendered
}

func (f *ResponseFormatter) FormatMajorEventUnsubscribed(ctx context.Context) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventUnsubscribed, majorEventSubscribedData{Emoji: DefaultEmoji})
	if err != nil {
		return ErrorMessage(ErrDisplayMajorEventFailed)
	}
	return rendered
}

func (f *ResponseFormatter) FormatMajorEventAlreadySubscribed(ctx context.Context) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventAlreadySub, majorEventSubscribedData{Emoji: DefaultEmoji})
	if err != nil {
		return ErrorMessage(ErrDisplayMajorEventFailed)
	}
	return rendered
}

func (f *ResponseFormatter) FormatMajorEventNotSubscribed(ctx context.Context) string {
	data := majorEventSubscribedData{Emoji: DefaultEmoji, Prefix: f.prefix}
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventNotSub, data)
	if err != nil {
		return ErrorMessage(ErrDisplayMajorEventFailed)
	}
	return rendered
}

func (f *ResponseFormatter) FormatMajorEventStatus(ctx context.Context, isSubscribed bool) string {
	data := majorEventStatusData{
		Emoji:        DefaultEmoji,
		IsSubscribed: isSubscribed,
		Prefix:       f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventStatus, data)
	if err != nil {
		return ErrorMessage(ErrDisplayMajorEventFailed)
	}
	return rendered
}

func (f *ResponseFormatter) FormatMajorEventUsage(ctx context.Context) string {
	data := majorEventUsageData{
		Emoji:  DefaultEmoji,
		Prefix: f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventUsage, data)
	if err != nil {
		return ErrorMessage(ErrDisplayMajorEventFailed)
	}
	return rendered
}

func formatMajorEventDates(dates []time.Time) string {
	if len(dates) == 0 {
		return "TBA"
	}

	weekdays := []string{"일", "월", "화", "수", "목", "금", "토"}
	formatDate := func(t time.Time) string {
		return fmt.Sprintf("%d년 %d월 %d일(%s)", t.Year(), t.Month(), t.Day(), weekdays[t.Weekday()])
	}

	if len(dates) == 1 {
		return formatDate(dates[0])
	}

	return fmt.Sprintf("%s ~ %s", formatDate(dates[0]), formatDate(dates[len(dates)-1]))
}

// formatMajorEventDatesFromDB는 DB에서 조회된 EventStartDate/EventEndDate를 기반으로 날짜 문자열을 생성합니다.
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
