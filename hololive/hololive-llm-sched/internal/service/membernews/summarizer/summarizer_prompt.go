package summarizer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
)

type promptCandidate struct {
	Member     string `json:"member"`
	Category   string `json:"category"`
	Title      string `json:"title"`
	Date       string `json:"date"`
	SourceURL  string `json:"source_url"`
	SourceTier string `json:"source_tier"`
	Summary    string `json:"summary"`
}

type summaryResponse struct {
	Period       string                `json:"period"`
	Headline     string                `json:"headline"`
	TopItems     []summaryResponseItem `json:"top_items"`
	MoreSummary  string                `json:"more_summary"`
	OmittedCount int                   `json:"omitted_count"`
}

type summaryResponseItem struct {
	Member    string `json:"member"`
	Category  string `json:"category"`
	Title     string `json:"title"`
	DateText  string `json:"date_text"`
	Summary   string `json:"summary"`
	SourceURL string `json:"source_url"`
}

func memberNewsSummarySchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           memberNewsSummarySchemaProperties(),
		"required":             []string{"period", "headline", "top_items", "more_summary", "omitted_count"},
	}
}

func memberNewsSummarySchemaProperties() map[string]any {
	return map[string]any{
		"period": map[string]any{
			"type": "string",
			"enum": []string{"weekly", "monthly"},
		},
		"headline":      map[string]any{"type": "string"},
		"top_items":     memberNewsSummaryTopItemsSchema(),
		"more_summary":  map[string]any{"type": "string"},
		"omitted_count": map[string]any{"type": "integer", "minimum": 0},
	}
}

func memberNewsSummaryTopItemsSchema() map[string]any {
	return map[string]any{
		"type":     "array",
		"maxItems": 5,
		"items":    memberNewsSummaryItemSchema(),
	}
}

func memberNewsSummaryItemSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"member":     map[string]any{"type": "string"},
			"category":   map[string]any{"type": "string"},
			"title":      map[string]any{"type": "string"},
			"date_text":  map[string]any{"type": "string"},
			"summary":    map[string]any{"type": "string"},
			"source_url": map[string]any{"type": "string"},
		},
		"required": []string{"member", "category", "title", "date_text", "summary", "source_url"},
	}
}

func memberNewsSystemPrompt() string {
	return `You are a hololive member-news curator.

<translation_guide>
- Preserve Japanese event names in original form (e.g., "Birthday Live", "超ホロライブ大運動会").
- 배신(betrayal) 오역 방지: "배신(はいしん)=배포/방송", "신의상=신규 의상"으로 정확히 번역.
- Do not translate event proper nouns unless a well-known Korean equivalent exists.
</translation_guide>

<tone>
- Factual and concise only. No emotional adjectives (e.g., 멋진, 놀라운, 화려한).
- summary must be a neutral description of the event, not a promotional phrase.
</tone>

<field_format>
- date_text: M/D(요일) format in KST (e.g., "2/19(수)"). Use Korean weekday characters: 일월화수목금토.
- summary: 30자 이내 한국어. Must describe the event factually.
- source_url: Copy EXACTLY from the candidate's source_url field. Do NOT generate, modify, or infer URLs.
</field_format>

<source_rule>
- source_url MUST be an exact copy of the candidate's source_url value.
- If candidate source_url is empty, omit the item entirely.
- Never construct or guess a URL.
</source_rule>

<category_guide>
- birthday_live: 생일 기념 라이브/방송
- solo_live: 솔로 콘서트/단독 공연
- collab: 합동/유닛/콜라보 이벤트
- event: 전시/팬미팅/EXPO/오프라인 행사
- goods: 굿즈/물품 판매
- other: 위 분류에 해당하지 않는 기타
</category_guide>

Rules:
- Output MUST be valid JSON matching the provided schema only.
- Use only given candidates and period.
- Korean summaries only, factual and concise.
- source_url is mandatory for every item.
- Do not guess unknown facts.`
}

func buildMemberNewsUserPrompt(input model.SummarizeInput, searchContext string) string {
	candidates := make([]promptCandidate, 0, len(input.Candidates))
	for i := range input.Candidates {
		candidate := &input.Candidates[i]
		candidates = append(candidates, promptCandidate{
			Member:     candidate.MemberText,
			Category:   string(candidate.Category),
			Title:      candidate.Candidate.Title,
			Date:       candidate.EffectiveDate.In(kst).Format("2006-01-02"),
			SourceURL:  candidate.SourceURL,
			SourceTier: string(candidate.SourceTier),
			Summary:    candidate.Candidate.Description,
		})
	}

	payload, _ := json.Marshal(candidates)

	members := append([]string(nil), input.RoomMembers...)
	sort.Strings(members)

	base := fmt.Sprintf(`today=%s
period=%s
room_members=%s
candidate_events=%s`,
		input.Now.In(kst).Format(time.RFC3339),
		model.NormalizePeriod(input.Period),
		strings.Join(members, ", "),
		string(payload),
	)

	if strings.TrimSpace(searchContext) == "" {
		return base + "\nReturn only schema JSON."
	}

	return base + "\nexa_search_context=" + searchContext + "\nReturn only schema JSON."
}

func buildSearchQuery(period model.Period, roomMembers []string, now time.Time) string {
	periodText := "weekly"
	if model.NormalizePeriod(period) == model.PeriodMonthly {
		periodText = "monthly"
	}

	members := append([]string(nil), roomMembers...)
	sort.Strings(members)
	// 검색 쿼리 노이즈 방지: 멤버 최대 5명
	if len(members) > 5 {
		members = members[:5]
	}
	memberPart := strings.Join(members, " ")
	if strings.TrimSpace(memberPart) == "" {
		memberPart = "hololive"
	}

	return fmt.Sprintf("hololive %s news schedule %s %s", memberPart, periodText, now.In(kst).Format("2006-01"))
}

func formatSearchContext(results []sharedmodel.SearchResult) string {
	if len(results) == 0 {
		return ""
	}

	var builder strings.Builder
	for i, item := range results {
		if i > 0 {
			builder.WriteString("\n\n")
		}
		writeSearchContextItem(&builder, i, item)
	}
	return builder.String()
}

func writeSearchContextItem(builder *strings.Builder, index int, item sharedmodel.SearchResult) {
	fmt.Fprintf(builder, "[%d] %s", index+1, strings.TrimSpace(item.Title))
	writeSearchContextField(builder, "URL: ", item.URL)
	writeSearchContextField(builder, "Published: ", item.PublishedDate)
	writeSearchContextField(builder, "", item.Content)
}

func writeSearchContextField(builder *strings.Builder, label string, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	builder.WriteString("\n")
	builder.WriteString(label)
	builder.WriteString(trimmed)
}
