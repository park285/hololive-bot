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
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
)

var (
	kst = model.KST
)

// LLMClient: 구조화 JSON 응답 생성 인터페이스.
type LLMClient interface {
	GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error)
}

// Summarizer: LLM + hard validator + deterministic fallback 요약기.
type SummarizerImpl struct {
	llm       LLMClient
	searcher  sharedmodel.WebSearcher
	validator model.SourceURLValidator
	logger    *slog.Logger
}

// NewSummarizer: 요약기 생성.
func NewSummarizer(
	llm LLMClient,
	searcher sharedmodel.WebSearcher,
	validator model.SourceURLValidator,
	logger *slog.Logger,
) *SummarizerImpl {
	if logger == nil {
		logger = slog.Default()
	}
	return &SummarizerImpl{
		llm:       llm,
		searcher:  searcher,
		validator: validator,
		logger:    logger,
	}
}

// Summarize: 요약 생성(실패 시 deterministic fallback).
func (s *SummarizerImpl) Summarize(ctx context.Context, input model.SummarizeInput) (*model.Digest, error) {
	if len(input.Candidates) == 0 {
		return newEmptyDigest(input.Period, 0), nil
	}

	if s == nil || s.llm == nil {
		return BuildDeterministicFallback(input.Period, input.Candidates), nil
	}

	searchContext := ""
	if s.searcher != nil {
		query := buildSearchQuery(input.Period, input.RoomMembers, input.Now)
		results, err := s.searcher.Search(ctx, query)
		if err != nil {
			s.logger.Warn("MemberNews Exa search failed (graceful)", slog.Any("error", err))
		} else {
			searchContext = formatSearchContext(results)
		}
	}

	raw, err := s.llm.GenerateJSON(ctx, memberNewsSystemPrompt(), buildMemberNewsUserPrompt(input, searchContext), memberNewsSummarySchema())
	if err != nil {
		s.logger.Warn("MemberNews LLM failed, using fallback", slog.String("error", err.Error()))
		return BuildDeterministicFallback(input.Period, input.Candidates), nil
	}

	var response summaryResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		s.logger.Warn("MemberNews schema parse failed, using fallback", slog.String("error", err.Error()))
		return BuildDeterministicFallback(input.Period, input.Candidates), nil
	}

	digest := s.validateAndBuildDigest(input, &response)
	if len(digest.TopItems) == 0 {
		s.logger.Warn("MemberNews validator dropped all items, using fallback")
		return BuildDeterministicFallback(input.Period, input.Candidates), nil
	}

	return digest, nil
}

func newEmptyDigest(period model.Period, totalCount int) *model.Digest {
	return &model.Digest{
		ResultType:   sharedmodel.SummaryResultEmpty,
		Period:       period,
		Headline:     model.DefaultHeadline(period),
		TopItems:     []model.SummaryItem{},
		MoreSummary:  "",
		OmittedCount: 0,
		TotalCount:   totalCount,
	}
}

// validateAndBuildDigest: LLM 출력을 재검증합니다.
// FilterCandidates에서 입력 후보를 사전 검증하지만, LLM이 source_url을 변형하거나
// 허용 범위 외 항목을 생성할 수 있으므로 출력에 대한 이중 검증이 의도적으로 적용됩니다.
func (s *SummarizerImpl) validateAndBuildDigest(input model.SummarizeInput, response *summaryResponse) *model.Digest {
	return validateAndBuildDigestFromResponse(input, response, s.validator)
}

// validateAndBuildDigestFromResponse: summaryResponse를 검증하여 Digest를 생성한다.
// SummarizerImpl.validateAndBuildDigest와 ConsensusSummarizer 양쪽에서 재사용.
func validateAndBuildDigestFromResponse(
	input model.SummarizeInput,
	response *summaryResponse,
	validator model.SourceURLValidator,
) *model.Digest {
	validatedItems := make([]model.SummaryItem, 0, len(response.TopItems))

	for i := range response.TopItems {
		item := &response.TopItems[i]
		if strings.TrimSpace(item.Member) == "" ||
			strings.TrimSpace(item.Category) == "" ||
			strings.TrimSpace(item.Title) == "" ||
			strings.TrimSpace(item.DateText) == "" ||
			strings.TrimSpace(item.Summary) == "" ||
			strings.TrimSpace(item.SourceURL) == "" {
			continue
		}

		normalizedCategory := normalizeCategory(item.Category)
		normalizedURL := strings.TrimSpace(item.SourceURL)

		if validator != nil {
			validatedTier, validatedSourceURL, err := validator.ValidateSourceURL(item.SourceURL)
			if err != nil {
				continue
			}
			normalizedURL = validatedSourceURL
			if validatedTier == model.SourceTierCommunity && !validator.HasCorroboration(item.Summary) {
				continue
			}
		}

		validatedItems = append(validatedItems, model.SummaryItem{
			Member:    strings.TrimSpace(item.Member),
			Category:  string(normalizedCategory),
			Title:     strings.TrimSpace(item.Title),
			DateText:  strings.TrimSpace(item.DateText),
			Summary:   strings.TrimSpace(item.Summary),
			SourceURL: normalizedURL,
		})
	}

	if len(validatedItems) > 5 {
		validatedItems = validatedItems[:5]
	}

	headline := strings.TrimSpace(response.Headline)
	if headline == "" {
		headline = model.DefaultHeadline(input.Period)
	}

	remaining := max(len(input.Candidates)-len(validatedItems), 0)
	// omitted_count는 LLM 출력이 아닌 서버 계산값을 SSOT로 사용해 일관성을 보장한다.
	omittedCount := remaining

	moreSummary := strings.TrimSpace(response.MoreSummary)
	if moreSummary == "" && omittedCount > 0 {
		moreSummary = fmt.Sprintf("외 %d건", omittedCount)
	}

	return &model.Digest{
		ResultType:   sharedmodel.SummaryResultPrimary,
		Period:       model.NormalizePeriod(model.Period(response.Period)),
		Headline:     headline,
		TopItems:     validatedItems,
		MoreSummary:  moreSummary,
		OmittedCount: omittedCount,
		TotalCount:   len(input.Candidates),
	}
}

// weekdayKR: 요일 한국어 레이블 (0=일요일 기준).
var weekdayKR = [...]string{"일", "월", "화", "수", "목", "금", "토"}

// categoryLabel: Category를 한국어 레이블로 변환.
func categoryLabel(cat model.Category) string {
	switch cat {
	case model.CategoryBirthdayLive:
		return "생일 라이브"
	case model.CategorySoloLive:
		return "솔로 라이브"
	case model.CategoryCollab:
		return "콜라보"
	case model.CategoryEvent:
		return "이벤트"
	case model.CategoryGoods:
		return "굿즈"
	default:
		return "기타"
	}
}

// BuildDeterministicFallback: LLM 실패/검증 실패 시 고정 규칙 출력 생성.
func BuildDeterministicFallback(period model.Period, candidates []model.FilteredCandidate) *model.Digest {
	items := make([]model.SummaryItem, 0, min(5, len(candidates)))
	for idx := range candidates {
		if idx >= 5 {
			break
		}
		candidate := &candidates[idx]

		localTime := candidate.EffectiveDate.In(kst)
		dateText := fmt.Sprintf("%d/%d(%s)", localTime.Month(), localTime.Day(), weekdayKR[localTime.Weekday()])
		summary := fmt.Sprintf("%s %s - %s", dateText, categoryLabel(candidate.Category), candidate.Candidate.Title)
		items = append(items, model.SummaryItem{
			Member:    candidate.MemberText,
			Category:  string(candidate.Category),
			Title:     candidate.Candidate.Title,
			DateText:  dateText,
			Summary:   summary,
			SourceURL: candidate.SourceURL,
		})
	}

	omitted := 0
	if len(candidates) > len(items) {
		omitted = len(candidates) - len(items)
	}

	moreSummary := ""
	if omitted > 0 {
		moreSummary = fmt.Sprintf("외 %d건", omitted)
	}

	return &model.Digest{
		ResultType:   sharedmodel.SummaryResultFallback,
		Period:       model.NormalizePeriod(period),
		Headline:     model.DefaultHeadline(period),
		TopItems:     items,
		MoreSummary:  moreSummary,
		OmittedCount: omitted,
		TotalCount:   len(candidates),
	}
}

func normalizeCategory(raw string) model.Category {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	switch normalized {
	case string(model.CategoryBirthdayLive):
		return model.CategoryBirthdayLive
	case string(model.CategorySoloLive):
		return model.CategorySoloLive
	case string(model.CategoryCollab):
		return model.CategoryCollab
	case string(model.CategoryEvent):
		return model.CategoryEvent
	case string(model.CategoryGoods):
		return model.CategoryGoods
	default:
		return model.CategoryOther
	}
}

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
		"properties": map[string]any{
			"period": map[string]any{
				"type": "string",
				"enum": []string{"weekly", "monthly"},
			},
			"headline": map[string]any{"type": "string"},
			"top_items": map[string]any{
				"type":     "array",
				"maxItems": 5,
				"items": map[string]any{
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
				},
			},
			"more_summary": map[string]any{"type": "string"},
			"omitted_count": map[string]any{
				"type":    "integer",
				"minimum": 0,
			},
		},
		"required": []string{"period", "headline", "top_items", "more_summary", "omitted_count"},
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
		fmt.Fprintf(&builder, "[%d] %s", i+1, strings.TrimSpace(item.Title))
		if strings.TrimSpace(item.URL) != "" {
			builder.WriteString("\nURL: ")
			builder.WriteString(strings.TrimSpace(item.URL))
		}
		if strings.TrimSpace(item.PublishedDate) != "" {
			builder.WriteString("\nPublished: ")
			builder.WriteString(strings.TrimSpace(item.PublishedDate))
		}
		if strings.TrimSpace(item.Content) != "" {
			builder.WriteString("\n")
			builder.WriteString(strings.TrimSpace(item.Content))
		}
	}
	return builder.String()
}
