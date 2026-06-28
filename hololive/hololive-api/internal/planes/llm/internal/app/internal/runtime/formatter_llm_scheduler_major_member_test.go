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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func TestFormatMajorEventWeeklySummary_EmptyEvents(t *testing.T) {
	t.Parallel()

	formatter := newLLMSchedulerFormatter("!", nil, nil)
	got := formatter.FormatMajorEventWeeklySummary(context.Background(), nil, "")
	assert.Equal(t, "", got)
}

func TestFormatMajorEventWeeklySummary_NoSeeMorePadding(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterRenderer(
		t,
		domain.TemplateKeyCmdMajorEventWeeklySummary,
		"주간 행사 안내\n이벤트 수: {{.Count}}",
	)
	formatter := newLLMSchedulerFormatter("!", renderer, nil)

	events := []domain.MajorEvent{
		{Title: "Holo Expo"},
		{Title: "Holo Fes"},
	}

	got := formatter.FormatMajorEventWeeklySummary(context.Background(), events, "")
	assert.Contains(t, got, "주간 행사 안내")
	assert.Contains(t, got, "이벤트 수: 2")
	assert.NotContains(t, got, "\u200b")
}

func TestFormatMajorEventWeeklySummary_UsesLLMSummaryWithoutFallbackList(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterRenderer(
		t,
		domain.TemplateKeyCmdMajorEventWeeklySummary,
		"주간 행사 안내\n요약={{.LLMSummary}}\n뷰수={{len .Events}}",
	)
	formatter := newLLMSchedulerFormatter("!", renderer, nil)

	events := []domain.MajorEvent{{Title: "A"}}
	got := formatter.FormatMajorEventWeeklySummary(context.Background(), events, "요약 본문")
	assert.Contains(t, got, "요약=요약 본문")
	assert.Contains(t, got, "뷰수=0")
}

func TestFormatMajorEventMonthlySummary_RenderFailFallback(t *testing.T) {
	t.Parallel()

	formatter := newLLMSchedulerFormatter("!", nil, nil)
	events := []domain.MajorEvent{{Title: "A"}}
	got := formatter.FormatMajorEventMonthlySummary(context.Background(), events, "")
	assert.Equal(t, messagestrings.FallbackSentinel, got)
}

func TestFormatMajorEventWeeklySummary_RenderFailFallback(t *testing.T) {
	t.Parallel()

	formatter := newLLMSchedulerFormatter("!", nil, nil)
	events := []domain.MajorEvent{{Title: "A"}}
	got := formatter.FormatMajorEventWeeklySummary(context.Background(), events, "")
	assert.Equal(t, messagestrings.FallbackSentinel, got)
}

func TestFormatMajorEventSummary_WeeklyMonthlyParity(t *testing.T) {
	t.Parallel()

	const body = "행사 안내\n수={{.Count}}\n요약={{.LLMSummary}}\n뷰수={{len .Events}}"
	renderer := setupFormatterRendererMulti(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMajorEventWeeklySummary:  body,
		domain.TemplateKeyCmdMajorEventMonthlySummary: body,
	})
	formatter := newLLMSchedulerFormatter("!", renderer, nil)

	events := []domain.MajorEvent{{Title: "A"}, {Title: "B"}}

	for _, llmSummary := range []string{"", "요약 본문"} {
		weekly := formatter.FormatMajorEventWeeklySummary(context.Background(), events, llmSummary)
		monthly := formatter.FormatMajorEventMonthlySummary(context.Background(), events, llmSummary)
		assert.Equal(t, weekly, monthly, "weekly/monthly must be identical modulo template key (llmSummary=%q)", llmSummary)
	}
}

func TestBuildMajorEventViewsAndDateFormatting(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	events := []domain.MajorEvent{
		{
			Title:          "Range Event",
			EventStartDate: &start,
			EventEndDate:   &end,
			Members:        []string{"A", "B"},
			Link:           "https://example.com/range",
		},
		{
			Title:   "TBA Event",
			Members: []string{"C"},
			Link:    "https://example.com/tba",
		},
	}

	views := buildMajorEventViews(events)
	require.Len(t, views, 2)

	assert.Equal(t, "Range Event", views[0].Title)
	assert.Contains(t, views[0].DateStr, "~")
	assert.True(t, views[0].HasDates)
	assert.Equal(t, "A, B", views[0].Members)

	assert.Equal(t, "TBA", views[1].DateStr)
	assert.False(t, views[1].HasDates)
}

func TestFormatMajorEventDatesFromDB(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, "TBA", formatMajorEventDatesFromDB(nil, nil))
	assert.Contains(t, formatMajorEventDatesFromDB(&start, nil), "2026년 3월 6일")
	assert.Contains(t, formatMajorEventDatesFromDB(&start, &start), "2026년 3월 6일")
	assert.Contains(t, formatMajorEventDatesFromDB(&start, &end), "~")
}

func TestFormatMemberNewsDigest(t *testing.T) {
	t.Parallel()

	t.Run("nil digest", func(t *testing.T) {
		t.Parallel()

		formatter := newLLMSchedulerFormatter("!", nil, nil)
		got := formatter.FormatMemberNewsDigest(context.Background(), nil)
		assert.Equal(t, messagestrings.FallbackSentinel, got)
	})

	t.Run("localize categories", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterRenderer(
			t,
			domain.TemplateKeyCmdMemberNewsDigest,
			"{{.Headline}}\n{{range .TopItems}}{{.Category}}|{{.Title}}\n{{end}}",
		)
		formatter := newLLMSchedulerFormatter("!", renderer, nil)
		formatter.store = setupMemberNewsStore(t)

		digest := &membernews.Digest{
			Headline: "이번주 뉴스",
			TopItems: []membernews.SummaryItem{
				{Category: "collab", Title: "합방"},
				{Category: "other", Title: "기타"},
			},
		}

		got := formatter.FormatMemberNewsDigest(context.Background(), digest)
		assert.Contains(t, got, "이번주 뉴스")
		assert.Contains(t, got, "콜라보|합방")
		assert.Contains(t, got, "기타|기타")
	})
}

func TestLocalizeMemberNewsItemsAndCategoryLabel(t *testing.T) {
	t.Parallel()

	items := []membernews.SummaryItem{
		{Category: "birthday_live", Title: "A"},
		{Category: "solo_live", Title: "B"},
		{Category: "event", Title: "C"},
		{Category: "unknown_code", Title: "D"},
	}

	formatter := newLLMSchedulerFormatter("!", nil, nil)
	formatter.store = setupMemberNewsStore(t)

	localized := formatter.localizeMemberNewsItems(t.Context(), items)
	require.Len(t, localized, 4)
	assert.Equal(t, "생일 라이브", localized[0].Category)
	assert.Equal(t, "솔로 라이브", localized[1].Category)
	assert.Equal(t, "이벤트", localized[2].Category)
	assert.Equal(t, "unknown_code", localized[3].Category)

	assert.Equal(t, "콜라보", formatter.memberNewsCategoryLabel(t.Context(), "collab"))
	assert.Equal(t, "굿즈", formatter.memberNewsCategoryLabel(t.Context(), "goods"))
	assert.Equal(t, "기타", formatter.memberNewsCategoryLabel(t.Context(), "other"))
	assert.Equal(t, "custom", formatter.memberNewsCategoryLabel(t.Context(), "custom"))
}
