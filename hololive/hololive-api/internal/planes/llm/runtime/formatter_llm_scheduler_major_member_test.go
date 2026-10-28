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
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
)

const seedBodyMajorEventWeeklySummary = `📅 이번 주 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if gt $index 0}}

{{- end}}
{{add $index 1}}. {{$event.Title}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{$event.Members}}
{{- end}}
{{- if $event.Link}}
   {{$event.Link}}
{{- end}}
{{- end}}`

const seedBodyMajorEventMonthlySummary = `📅 이번 달 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if gt $index 0}}

{{- end}}
{{add $index 1}}. {{$event.Title}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{$event.Members}}
{{- end}}
{{- if $event.Link}}
   {{$event.Link}}
{{- end}}
{{- end}}`

const seedBodyMemberNewsDigest = `{{- if .Headline -}}
{{.Headline}}
{{- else -}}
📰 멤버 뉴스
{{- end -}}
{{- if eq (len .TopItems) 0 }}
표시할 뉴스가 없습니다.
{{- else }}
{{range $index, $item := .TopItems}}
{{- if gt $index 0 }}

{{- end -}}
{{add $index 1}}. [{{$item.DateText}}] {{$item.Member}} · {{$item.Category}}
   {{$item.Title}}
   {{- if $item.Summary}}
   {{$item.Summary}}
   {{- end}}
   {{$item.SourceURL}}
{{- end}}
{{- if .MoreSummary }}

{{.MoreSummary}}
{{- end }}
{{- end }}`

func TestFormatMajorEventWeeklySummary_EmptyEvents(t *testing.T) {
	t.Parallel()

	formatter := newLLMSchedulerFormatter("!", nil, nil, false)
	got, err := formatter.FormatMajorEventWeeklySummary(t.Context(), nil, "")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestFormatMajorEventWeeklySummary_NoSeeMorePadding(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterRenderer(
		t,
		domain.TemplateKeyCmdMajorEventWeeklySummary,
		seedBodyMajorEventWeeklySummary,
	)
	formatter := newLLMSchedulerFormatter("!", renderer, nil, false)

	events := []domain.MajorEvent{
		{Title: "Holo Expo"},
		{Title: "Holo Fes"},
	}

	got, err := formatter.FormatMajorEventWeeklySummary(t.Context(), events, "")
	require.NoError(t, err)
	assert.Contains(t, got, "📅 이번 주 행사 (2)")
	assert.Contains(t, got, "1. Holo Expo")
	assert.Contains(t, got, "2. Holo Fes")
	assert.NotContains(t, got, "\u200b")
}

func TestFormatMajorEventWeeklySummary_UsesLLMSummaryWithoutFallbackList(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterRenderer(
		t,
		domain.TemplateKeyCmdMajorEventWeeklySummary,
		seedBodyMajorEventWeeklySummary,
	)
	formatter := newLLMSchedulerFormatter("!", renderer, nil, false)

	events := []domain.MajorEvent{{Title: "A"}}
	got, err := formatter.FormatMajorEventWeeklySummary(t.Context(), events, "요약 본문")
	require.NoError(t, err)
	assert.Contains(t, got, "📅 이번 주 행사 (1)")
	assert.Contains(t, got, "요약 본문")
	assert.NotContains(t, got, "1. A")
}

// 예약 알림 렌더 실패는 코드 대체 문구를 구독 방에 보내지 않고 오류로 돌려준다.
func TestFormatMajorEventMonthlySummary_RenderFailReturnsError(t *testing.T) {
	t.Parallel()

	formatter := newLLMSchedulerFormatter("!", nil, nil, false)
	events := []domain.MajorEvent{{Title: "A"}}
	got, err := formatter.FormatMajorEventMonthlySummary(t.Context(), events, "")
	require.Error(t, err)
	assert.Empty(t, got)
}

func TestFormatMajorEventWeeklySummary_RenderFailReturnsError(t *testing.T) {
	t.Parallel()

	formatter := newLLMSchedulerFormatter("!", nil, nil, false)
	events := []domain.MajorEvent{{Title: "A"}}
	got, err := formatter.FormatMajorEventWeeklySummary(t.Context(), events, "")
	require.Error(t, err)
	assert.Empty(t, got)
}

func TestFormatMajorEventSummary_WeeklyMonthlyParity(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterRendererMulti(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMajorEventWeeklySummary:  seedBodyMajorEventWeeklySummary,
		domain.TemplateKeyCmdMajorEventMonthlySummary: seedBodyMajorEventMonthlySummary,
	})
	formatter := newLLMSchedulerFormatter("!", renderer, nil, false)

	events := []domain.MajorEvent{{Title: "A"}, {Title: "B"}}

	for _, llmSummary := range []string{"", "요약 본문"} {
		weekly, weeklyErr := formatter.FormatMajorEventWeeklySummary(t.Context(), events, llmSummary)
		require.NoError(t, weeklyErr)

		monthly, monthlyErr := formatter.FormatMajorEventMonthlySummary(t.Context(), events, llmSummary)
		require.NoError(t, monthlyErr)

		normalizedWeekly := strings.Replace(weekly, "이번 주 행사", "이번 달 행사", 1)
		assert.Equal(t, normalizedWeekly, monthly, "weekly/monthly must be identical modulo header word (llmSummary=%q)", llmSummary)
	}
}

func TestFormatMemberNewsDigest(t *testing.T) {
	t.Parallel()

	t.Run("nil digest", func(t *testing.T) {
		t.Parallel()

		formatter := newLLMSchedulerFormatter("!", nil, nil, false)
		got, err := formatter.FormatMemberNewsDigest(t.Context(), nil)
		require.Error(t, err)
		assert.Empty(t, got)
	})

	t.Run("normal empty digest", func(t *testing.T) {
		t.Parallel()

		formatter := newLLMSchedulerFormatter("!", setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, seedBodyMemberNewsDigest), nil, true)
		got, err := formatter.FormatMemberNewsDigest(t.Context(), &model.Digest{Headline: "뉴스"})
		require.NoError(t, err)
		assert.Contains(t, got, "표시할 뉴스가 없습니다.")
		assert.NotContains(t, got, strings.Repeat("\u200b", 500))
	})

	t.Run("localize categories", func(t *testing.T) {
		t.Parallel()

		renderer := setupFormatterRenderer(
			t,
			domain.TemplateKeyCmdMemberNewsDigest,
			seedBodyMemberNewsDigest,
		)
		formatter := newLLMSchedulerFormatter("!", renderer, nil, false)

		formatter.store = setupMemberNewsStore(t)

		digest := &model.Digest{
			Headline: "이번주 뉴스",
			TopItems: []model.SummaryItem{
				{Category: "collab", Title: "합방"},
				{Category: "other", Title: "기타"},
			},
		}

		got, err := formatter.FormatMemberNewsDigest(t.Context(), digest)
		require.NoError(t, err)
		assert.Contains(t, got, "이번주 뉴스")
		assert.Contains(t, got, "· 콜라보")
		assert.Contains(t, got, "· 기타")
		assert.Contains(t, got, "합방")
	})
}

func TestMajorEventSummaryLiteralBoundary(t *testing.T) {
	t.Parallel()

	const body = "**행사**\n\n{{.LLMSummary}}{{range .Events}}{{.Title}}{{end}}"
	renderer := setupFormatterRendererMulti(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMajorEventWeeklySummary:  body,
		domain.TemplateKeyCmdMajorEventMonthlySummary: body,
	})
	const summary = "공개 ||가격|| **원제** (_공식명_)\r\n- 무료 **안내** &amp;\r\nhttps://example.org/a_b*(c)?q=||x||&v=~y~\n!썸네일 a_b"
	events := []domain.MajorEvent{{Title: "중복되면 안 되는 목록"}, {Title: "다른 행사"}}
	padding := strings.Repeat(kakaotext.ZeroWidthSpace, templateview.KakaoSeeMorePadding)

	for _, fold := range []bool{false, true} {
		formatter := newLLMSchedulerFormatter("!", renderer, nil, fold)
		for _, format := range []func() (string, error){
			func() (string, error) {
				return formatter.FormatMajorEventWeeklySummary(t.Context(), events, summary+padding)
			},
			func() (string, error) {
				return formatter.FormatMajorEventMonthlySummary(t.Context(), events, summary+padding)
			},
		} {
			message, err := format()
			require.NoError(t, err)
			require.Equal(t, fold, strings.Contains(message, padding))
			visible := strings.ReplaceAll(kakaoformat.Render(message), padding, "")
			require.Contains(t, visible, strings.ReplaceAll(summary, "\r\n", "\n"))
			for _, event := range events {
				require.NotContains(t, visible, event.Title)
			}
		}
	}
}

func TestInvisibleSummaryKeepsEventList(t *testing.T) {
	t.Parallel()

	const body = "{{.LLMSummary}}{{range .Events}}{{.Title}}\n{{end}}"
	renderer := setupFormatterRendererMulti(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMajorEventWeeklySummary:  body,
		domain.TemplateKeyCmdMajorEventMonthlySummary: body,
	})
	formatter := newLLMSchedulerFormatter("!", renderer, nil, false)
	events := []domain.MajorEvent{{Title: "표시할 행사"}, {Title: "다음 행사"}}

	for _, format := range []func(string) (string, error){
		func(summary string) (string, error) {
			return formatter.FormatMajorEventWeeklySummary(t.Context(), events, summary)
		},
		func(summary string) (string, error) {
			return formatter.FormatMajorEventMonthlySummary(t.Context(), events, summary)
		},
	} {
		empty, err := format("")
		require.NoError(t, err)
		for _, event := range events {
			require.Contains(t, empty, event.Title)
		}
		for _, summary := range []string{
			kakaotext.ZeroWidthSpace,
			strings.Repeat(kakaotext.ZeroWidthSpace, 500),
			" \r\n\t" + kakaotext.ZeroWidthSpace + " \r \t" + kakaotext.ZeroWidthSpace,
		} {
			message, err := format(summary)
			require.NoError(t, err)
			require.Equal(t, empty, message)
		}
	}
}
