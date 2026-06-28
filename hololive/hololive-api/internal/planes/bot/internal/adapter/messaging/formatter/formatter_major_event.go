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
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	templateview "github.com/kapu/hololive-shared/pkg/templateview"
)

type majorEventWeeklySummaryData struct {
	Count      int
	Events     []majorEventView
	LLMSummary string
}

type majorEventMonthlySummaryData struct {
	Count      int
	Events     []majorEventView
	LLMSummary string
}

type majorEventView = templateview.MajorEventView

type majorEventSubscribedData struct {
	Prefix string
}

type majorEventStatusData struct {
	IsSubscribed bool
	Prefix       string
}

type majorEventUsageData struct {
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
		Count:      len(events),
		Events:     views,
		LLMSummary: normalizedSummary,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventWeeklySummary, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
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
		Count:      len(events),
		Events:     views,
		LLMSummary: normalizedSummary,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventMonthlySummary, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func buildMajorEventViews(events []domain.MajorEvent) []majorEventView {
	return templateview.BuildMajorEventViews(events)
}

func (f *ResponseFormatter) FormatMajorEventSubscribed(ctx context.Context) string {
	data := majorEventSubscribedData{
		Prefix: f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventSubscribed, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventUnsubscribed(ctx context.Context) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventUnsubscribed, majorEventSubscribedData{})
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventAlreadySubscribed(ctx context.Context) string {
	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventAlreadySub, majorEventSubscribedData{})
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventNotSubscribed(ctx context.Context) string {
	data := majorEventSubscribedData{Prefix: f.prefix}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventNotSub, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventStatus(ctx context.Context, isSubscribed bool) string {
	data := majorEventStatusData{
		IsSubscribed: isSubscribed,
		Prefix:       f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventStatus, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) FormatMajorEventUsage(ctx context.Context) string {
	data := majorEventUsageData{
		Prefix: f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdMajorEventUsage, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}
