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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRenderNotification_SeeMoreFoldToggle(t *testing.T) {
	t.Parallel()

	longBody := "다이제스트 헤더\n" + strings.Repeat("뉴스 항목 행입니다\n", 40)
	renderer := setupFormatterRenderer(t, domain.TemplateKeyCmdMemberNewsDigest, longBody)

	on := newLLMSchedulerFormatter("!", renderer, nil, true)
	folded, err := on.renderNotification(t.Context(), domain.TemplateKeyCmdMemberNewsDigest, nil, "warn", true)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(folded, "다이제스트 헤더\u200b"), "padding must follow the head line")
	assert.Contains(t, folded, "\u200b")

	off := newLLMSchedulerFormatter("!", renderer, nil, false)
	unfolded, err := off.renderNotification(t.Context(), domain.TemplateKeyCmdMemberNewsDigest, nil, "warn", true)
	require.NoError(t, err)
	assert.NotContains(t, unfolded, "\u200b")
}

func TestSchedulerReportFoldingUsesDisplayedCount(t *testing.T) {
	for _, body := range []string{"제목\n\n본문", "제목\n\n" + strings.Repeat("긴 본문", 200)} {
		renderer := setupFormatterRendererMulti(t, map[domain.TemplateKey]string{
			domain.TemplateKeyCmdMemberNewsDigest: body, domain.TemplateKeyCmdMajorEventWeeklySummary: body, domain.TemplateKeyCmdMajorEventMonthlySummary: body,
		})

		for _, tc := range []struct {
			count     int
			more      string
			newsFold  bool
			eventFold bool
		}{
			{count: 0}, {count: 1}, {count: 1, more: "추가 요약\n다음 줄", newsFold: true}, {count: 2, newsFold: true, eventFold: true},
		} {
			t.Run(fmt.Sprintf("%d/%t/%d", tc.count, tc.more != "", len(body)), func(t *testing.T) {
				for _, enabled := range []bool{false, true} {
					f := newLLMSchedulerFormatter("!", renderer, nil, enabled)
					digest := &model.Digest{TopItems: make([]model.SummaryItem, tc.count), MoreSummary: tc.more, TotalCount: 100}
					news, err := f.FormatMemberNewsDigest(t.Context(), digest)
					require.NoError(t, err)
					assert.Equal(t, enabled && tc.newsFold, strings.Contains(news, strings.Repeat("\u200b", 500)))

					for _, summary := range []string{"", "LLM 요약"} {
						events := make([]domain.MajorEvent, tc.count)
						weekly, err := f.FormatMajorEventWeeklySummary(t.Context(), events, summary)
						require.NoError(t, err)

						monthly, err := f.FormatMajorEventMonthlySummary(t.Context(), events, summary)
						require.NoError(t, err)
						assert.Equal(t, weekly, monthly)

						if tc.count == 0 {
							assert.Empty(t, weekly)
						} else {
							assert.Equal(t, enabled && tc.eventFold, strings.Contains(weekly, strings.Repeat("\u200b", 500)))
							assert.Equal(t, body, strings.Replace(weekly, strings.Repeat("\u200b", 500), "", 1))
						}
					}
				}
			})
		}
	}
}

func TestSchedulerReportsRejectWhitespaceRendering(t *testing.T) {
	renderer := setupFormatterRendererMulti(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMemberNewsDigest: " \n\t", domain.TemplateKeyCmdMajorEventWeeklySummary: " \n\t", domain.TemplateKeyCmdMajorEventMonthlySummary: " \n\t",
	})
	f := newLLMSchedulerFormatter("!", renderer, nil, true)
	news, err := f.FormatMemberNewsDigest(t.Context(), &model.Digest{})
	require.ErrorContains(t, err, "template rendered empty")
	assert.Empty(t, news)

	weekly, err := f.FormatMajorEventWeeklySummary(t.Context(), []domain.MajorEvent{{Title: "행사"}}, "")
	require.ErrorContains(t, err, "template rendered empty")
	assert.Empty(t, weekly)

	monthly, err := f.FormatMajorEventMonthlySummary(t.Context(), []domain.MajorEvent{{Title: "행사"}}, "")
	require.ErrorContains(t, err, "template rendered empty")
	assert.Empty(t, monthly)
}
