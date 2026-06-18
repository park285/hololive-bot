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

package summarizer

import (
	"fmt"
	"strings"

	"github.com/kapu/hololive-llm-sched/internal/model"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func buildSearchQuery(summaryType SummaryType, periodKey string) string {
	sourceScope := buildORScope("site:", constants.MajorEventConfig.SearchSourceSites)
	accountScope := buildORScope("", constants.MajorEventConfig.SearchOfficialAccounts)
	partnerScope := buildORScope("", constants.MajorEventConfig.SearchPartnerKeywords)

	switch summaryType {
	case SummaryTypeMonthly:
		return fmt.Sprintf("hololive production events %s month schedule %s %s %s", periodKey, sourceScope, accountScope, partnerScope)
	case SummaryTypeWeekly:
		return fmt.Sprintf("hololive production events schedule %s week %s %s %s", periodKey, sourceScope, accountScope, partnerScope)
	default:
		return fmt.Sprintf("hololive production events schedule %s week %s %s %s", periodKey, sourceScope, accountScope, partnerScope)
	}
}

func buildORScope(prefix string, items []string) string {
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, prefix+trimmed)
	}
	if len(filtered) == 0 {
		return ""
	}
	return "(" + strings.Join(filtered, " OR ") + ")"
}

// buildKRPartnerSearchQuery: 한국 파트너 이벤트 전용 검색 쿼리
func buildKRPartnerSearchQuery(periodKey string) string {
	return fmt.Sprintf(
		"ANIPLUS hololive live viewing %s (site:aniplus.co.kr OR ANIPLUS_SHOP OR v_square_kr OR AGF_KOREA)",
		periodKey,
	)
}

const maxSearchResults = 10

// capSearchResults: 검색 결과를 최대 개수로 제한
func capSearchResults(results []model.SearchResult, limit int) []model.SearchResult {
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}

// dedupeSearchResults: URL 기반 중복 제거 (URL 없으면 Title+PublishedDate 복합키)
func dedupeSearchResults(results []model.SearchResult) []model.SearchResult {
	seen := make(map[string]struct{}, len(results))
	deduped := make([]model.SearchResult, 0, len(results))
	for _, r := range results {
		key := r.URL
		if key == "" {
			// URL 없을 때: title+publishedDate 복합키로 오병합 방지
			key = r.Title + "|" + r.PublishedDate
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, r)
	}
	return deduped
}

func formatSearchResults(results []model.SearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, result := range results {
		if i > 0 {
			sb.WriteString("\n\n")
		}

		writeSearchResultHeader(&sb, i+1, result.Title)
		writeSearchResultField(&sb, "출처", result.URL)
		writeSearchResultField(&sb, "기간", result.PublishedDate)
		writeSearchResultField(&sb, "내용", result.Content)
	}

	return sb.String()
}

func writeSearchResultHeader(sb *strings.Builder, index int, title string) {
	if title != "" {
		fmt.Fprintf(sb, "[%d] %s", index, title)
		return
	}
	fmt.Fprintf(sb, "[%d]", index)
}

func writeSearchResultField(sb *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	sb.WriteString("\n")
	sb.WriteString(label)
	sb.WriteString(": ")
	sb.WriteString(value)
}
