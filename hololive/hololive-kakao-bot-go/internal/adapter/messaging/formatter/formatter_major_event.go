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

package formatter

import (
	"context"
	"fmt"
	"strings"
	"time"

	msging "github.com/kapu/hololive-kakao-bot-go/internal/adapter/messaging"
	"github.com/kapu/hololive-shared/pkg/domain"
	templateview "github.com/kapu/hololive-shared/pkg/templateview"
	"github.com/kapu/hololive-shared/pkg/util"
)

type majorEventWeeklySummaryData struct {
	Emoji      msging.UIEmoji
	Count      int
	Events     []majorEventView
	LLMSummary string
}

type majorEventMonthlySummaryData struct {
	Emoji      msging.UIEmoji
	Count      int
	Events     []majorEventView
	LLMSummary string
}

type majorEventView = templateview.MajorEventView

type majorEventSubscribedData struct {
	Emoji  msging.UIEmoji
	Prefix string
}

type majorEventStatusData struct {
	Emoji        msging.UIEmoji
	IsSubscribed bool
	Prefix       string
}

type majorEventUsageData struct {
	Emoji  msging.UIEmoji
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
		Emoji:      msging.DefaultEmoji,
		Count:      len(events),
		Events:     views,
		LLMSummary: normalizedSummary,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventWeeklySummary, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMajorEventFailed)
	}

	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}

	return util.ApplyKakaoSeeMorePadding(body, instruction)
}

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
		Emoji:      msging.DefaultEmoji,
		Count:      len(events),
		Events:     views,
		LLMSummary: normalizedSummary,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventMonthlySummary, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMajorEventFailed)
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

func (f *ResponseFormatter) FormatMajorEventSubscribed(ctx context.Context) string {
	data := majorEventSubscribedData{
		Emoji:  msging.DefaultEmoji,
		Prefix: f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventSubscribed, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMajorEventFailed)
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventUnsubscribed(ctx context.Context) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventUnsubscribed, majorEventSubscribedData{Emoji: msging.DefaultEmoji})
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMajorEventFailed)
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventAlreadySubscribed(ctx context.Context) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventAlreadySub, majorEventSubscribedData{Emoji: msging.DefaultEmoji})
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMajorEventFailed)
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventNotSubscribed(ctx context.Context) string {
	data := majorEventSubscribedData{Emoji: msging.DefaultEmoji, Prefix: f.prefix}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventNotSub, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMajorEventFailed)
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventStatus(ctx context.Context, isSubscribed bool) string {
	data := majorEventStatusData{
		Emoji:        msging.DefaultEmoji,
		IsSubscribed: isSubscribed,
		Prefix:       f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventStatus, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMajorEventFailed)
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventUsage(ctx context.Context) string {
	data := majorEventUsageData{
		Emoji:  msging.DefaultEmoji,
		Prefix: f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventUsage, data)
	if err != nil {
		return msging.ErrorMessage(msging.ErrDisplayMajorEventFailed)
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
	return templateview.FormatMajorEventDatesFromDB(start, end)
}
