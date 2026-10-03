package summarizer

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"time"

	sharedmodel "github.com/kapu/hololive-api/internal/planes/llm/internal/model"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
)

// JSON Schema 어휘 리터럴. 프로덕션 스키마 전용이며, golden 테스트는
// 프로덕션 값 변경을 감지해야 하므로 이 상수를 참조하지 않는다.
const (
	schemaKeyType    = "type"
	schemaTypeString = "string"
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
		schemaKeyType:          "object",
		"additionalProperties": false,
		"properties":           memberNewsSummarySchemaProperties(),
		"required":             []string{"period", "headline", "top_items", "more_summary", "omitted_count"},
	}
}

func memberNewsSummarySchemaProperties() map[string]any {
	return map[string]any{
		"period": map[string]any{
			schemaKeyType: schemaTypeString,
			"enum":        []string{"weekly", "monthly"},
		},
		"headline":      map[string]any{schemaKeyType: schemaTypeString},
		"top_items":     memberNewsSummaryTopItemsSchema(),
		"more_summary":  map[string]any{schemaKeyType: schemaTypeString},
		"omitted_count": map[string]any{schemaKeyType: "integer", "minimum": 0},
	}
}

func memberNewsSummaryTopItemsSchema() map[string]any {
	return map[string]any{
		schemaKeyType: "array",
		"maxItems":    5,
		"items":       memberNewsSummaryItemSchema(),
	}
}

func memberNewsSummaryItemSchema() map[string]any {
	return map[string]any{
		schemaKeyType:          "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"member":     map[string]any{schemaKeyType: schemaTypeString},
			"category":   map[string]any{schemaKeyType: schemaTypeString},
			"title":      map[string]any{schemaKeyType: schemaTypeString},
			"date_text":  map[string]any{schemaKeyType: schemaTypeString},
			"summary":    map[string]any{schemaKeyType: schemaTypeString},
			"source_url": map[string]any{schemaKeyType: schemaTypeString},
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

func buildMemberNewsUserPrompt(input *model.SummarizeInput, searchContext string) (string, error) {
	payload, err := marshalPromptJSON("candidate events", buildPromptCandidates(input))
	if err != nil {
		return "", err
	}

	members := append([]string(nil), input.RoomMembers...)
	slices.Sort(members)

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
		return base + "\nReturn only schema JSON.", nil
	}

	return base + "\nexa_search_context=" + searchContext + "\nReturn only schema JSON.", nil
}

// marshalPromptJSON은 prompt에 넣을 값을 직렬화한다. 실패를 빈 목록·null로 바꾸면 LLM이 입력 없이 요약하므로
// 오류를 그대로 돌려준다. 외부에서 수집한 제목의 잘못된 UTF-8이 대표적인 실패 원인이다.
func marshalPromptJSON(field string, value any) ([]byte, error) {
	data, err := jsonv2.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal prompt %s: %w", field, err)
	}

	return data, nil
}

func buildSearchQuery(period model.Period, roomMembers []string, now time.Time) string {
	periodText := "weekly"

	if model.NormalizePeriod(period) == model.PeriodMonthly {
		periodText = "monthly"
	}

	members := append([]string(nil), roomMembers...)
	slices.Sort(members)

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

func writeSearchContextField(builder *strings.Builder, label, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}

	builder.WriteString("\n")
	builder.WriteString(label)
	builder.WriteString(trimmed)
}
