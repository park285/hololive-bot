package summarizer

import (
	"fmt"
	"strings"

	"github.com/kapu/hololive-llm-sched/internal/model"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// WebSearcher: model.WebSearcher의 별칭 (패키지 내 참조 호환)
type WebSearcher = model.WebSearcher

// SearchResult: model.SearchResult의 별칭 (패키지 내 참조 호환)
type SearchResult = model.SearchResult

func buildSearchQuery(summaryType SummaryType, periodKey string) string {
	sourceScope := buildORScope("site:", constants.MajorEventConfig.SearchSourceSites)
	accountScope := buildORScope("", constants.MajorEventConfig.SearchOfficialAccounts)
	partnerScope := buildORScope("", constants.MajorEventConfig.SearchPartnerKeywords)

	switch summaryType {
	case SummaryTypeMonthly:
		return fmt.Sprintf("hololive production events %s month schedule %s %s %s", periodKey, sourceScope, accountScope, partnerScope)
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
func capSearchResults(results []SearchResult, limit int) []SearchResult {
	if len(results) <= limit {
		return results
	}
	return results[:limit]
}

// dedupeSearchResults: URL 기반 중복 제거 (URL 없으면 Title+PublishedDate 복합키)
func dedupeSearchResults(results []SearchResult) []SearchResult {
	seen := make(map[string]struct{}, len(results))
	deduped := make([]SearchResult, 0, len(results))
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

func formatSearchResults(results []SearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, result := range results {
		if i > 0 {
			sb.WriteString("\n\n")
		}

		if result.Title != "" {
			fmt.Fprintf(&sb, "[%d] %s", i+1, result.Title)
		} else {
			fmt.Fprintf(&sb, "[%d]", i+1)
		}

		if result.URL != "" {
			sb.WriteString("\n출처: ")
			sb.WriteString(result.URL)
		}
		if result.PublishedDate != "" {
			sb.WriteString("\n기간: ")
			sb.WriteString(result.PublishedDate)
		}
		if result.Content != "" {
			sb.WriteString("\n내용: ")
			sb.WriteString(result.Content)
		}
	}

	return sb.String()
}
