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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
	"github.com/park285/shared-go/v2/pkg/promptguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/guardrail"
	sharedmodel "github.com/kapu/hololive-api/internal/planes/llm/internal/model"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/util"
)

var kst = util.KSTZone

var categoryLabels = map[model.Category]string{
	model.CategoryBirthdayLive: "생일 라이브",
	model.CategorySoloLive:     "솔로 라이브",
	model.CategoryCollab:       "콜라보",
	model.CategoryEvent:        "이벤트",
	model.CategoryGoods:        "굿즈",
}

var normalizedCategories = map[string]model.Category{
	string(model.CategoryBirthdayLive): model.CategoryBirthdayLive,
	string(model.CategorySoloLive):     model.CategorySoloLive,
	string(model.CategoryCollab):       model.CategoryCollab,
	string(model.CategoryEvent):        model.CategoryEvent,
	string(model.CategoryGoods):        model.CategoryGoods,
}

// ErrLLMUnavailable은 요약 LLM client 없이 Summarize가 호출됐음을 뜻한다.
// ErrNoValidatedItems는 LLM 출력이 재검증에서 모두 버려졌음을 뜻한다.
// 요약기는 결정적 digest를 스스로 만들지 않고 이 오류들을 돌려준다. 결정적 fallback digest는 membernews.Service가
// 한 곳에서 소유한다(stack audit B5).
var (
	ErrLLMUnavailable   = errors.New("member news summarizer LLM unavailable")
	ErrNoValidatedItems = errors.New("member news summary has no validated items")
)

// LLMClient는 지시를 계층으로만 받는다(DEC-20260926-stack-llm-instruction-layering-sole-path).
// 요약·검토·판정 지시는 작업 절차·출력 형식이라 developer 계층이고, 후보·검색 결과는 user 계층이다.
type LLMClient interface {
	GenerateJSON(ctx context.Context, prompts openaipreset.PromptLayers, schema map[string]any) (string, error)
}

type SummarizerImpl struct {
	llm         LLMClient
	searcher    sharedmodel.WebSearcher
	validator   model.SourceURLValidator
	promptGuard *promptguard.Guard
	logger      *slog.Logger
}

type SummarizerOption func(*SummarizerImpl)

func WithPromptGuard(guard *promptguard.Guard) SummarizerOption {
	return func(summarizer *SummarizerImpl) {
		summarizer.promptGuard = guard
	}
}

func NewSummarizer(
	llm LLMClient,
	searcher sharedmodel.WebSearcher,
	validator model.SourceURLValidator,
	logger *slog.Logger,
	opts ...SummarizerOption,
) *SummarizerImpl {
	if logger == nil {
		logger = slog.Default()
	}

	summarizer := &SummarizerImpl{
		llm:       llm,
		searcher:  searcher,
		validator: validator,
		logger:    logger,
	}

	for _, opt := range opts {
		opt(summarizer)
	}

	return summarizer
}

func (s *SummarizerImpl) Summarize(ctx context.Context, input *model.SummarizeInput) (*model.Digest, error) {
	if input == nil {
		return newEmptyDigest(model.PeriodWeekly, 0), nil
	}

	if len(input.Candidates) == 0 {
		return newEmptyDigest(input.Period, 0), nil
	}

	if s == nil || s.llm == nil {
		return nil, ErrLLMUnavailable
	}

	searchContext, err := s.searchContext(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("member news search context: %w", err)
	}

	raw, err := s.llm.GenerateJSON(ctx, openaipreset.PromptLayers{
		Developer: memberNewsSystemPrompt(),
		User:      buildMemberNewsUserPrompt(input, searchContext),
	}, memberNewsSummarySchema())
	if err != nil {
		return nil, fmt.Errorf("generate member news summary: %w", err)
	}

	var response summaryResponse

	if err := jsonv2.Unmarshal([]byte(raw), &response); err != nil {
		return nil, fmt.Errorf("parse member news summary: %w", err)
	}

	digest := s.validateAndBuildDigest(input, &response)
	if len(digest.TopItems) == 0 {
		return nil, ErrNoValidatedItems
	}

	return digest, nil
}

func (s *SummarizerImpl) searchContext(ctx context.Context, input *model.SummarizeInput) (string, error) {
	if s.searcher == nil {
		return "", nil
	}

	query := buildSearchQuery(input.Period, input.RoomMembers, input.Now)

	results, err := s.searcher.Search(ctx, query)
	if err != nil {
		s.logger.Warn("MemberNews Exa search failed (graceful)", slog.Any("error", err))

		return "", nil
	}

	results, err = guardrail.FilterSearchResults(results, s.promptGuard, s.logger, "membernews_search")
	if err != nil {
		return "", fmt.Errorf("guard member news search results: %w", err)
	}

	return formatSearchContext(results), nil
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
func (s *SummarizerImpl) validateAndBuildDigest(input *model.SummarizeInput, response *summaryResponse) *model.Digest {
	return validateAndBuildDigestFromResponse(input, response, s.validator)
}

// validateAndBuildDigestFromResponse: summaryResponse를 검증하여 Digest를 생성한다.
// SummarizerImpl.validateAndBuildDigest와 ConsensusSummarizer 양쪽에서 재사용.
func validateAndBuildDigestFromResponse(
	input *model.SummarizeInput,
	response *summaryResponse,
	validator model.SourceURLValidator,
) *model.Digest {
	validatedItems := make([]model.SummaryItem, 0, len(response.TopItems))

	for i := range response.TopItems {
		appendValidatedSummaryItem(&validatedItems, &response.TopItems[i], validator)
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

func appendValidatedSummaryItem(items *[]model.SummaryItem, item *summaryResponseItem, validator model.SourceURLValidator) {
	if !summaryResponseItemHasRequiredFields(item) {
		return
	}

	normalizedURL, ok := validateSummaryResponseItemSource(item, validator)
	if !ok {
		return
	}

	*items = append(*items, model.SummaryItem{
		Member:    strings.TrimSpace(item.Member),
		Category:  string(normalizeCategory(item.Category)),
		Title:     strings.TrimSpace(item.Title),
		DateText:  strings.TrimSpace(item.DateText),
		Summary:   strings.TrimSpace(item.Summary),
		SourceURL: normalizedURL,
	})
}

func summaryResponseItemHasRequiredFields(item *summaryResponseItem) bool {
	return strings.TrimSpace(item.Member) != "" &&
		strings.TrimSpace(item.Category) != "" &&
		strings.TrimSpace(item.Title) != "" &&
		strings.TrimSpace(item.DateText) != "" &&
		strings.TrimSpace(item.Summary) != "" &&
		strings.TrimSpace(item.SourceURL) != ""
}

func validateSummaryResponseItemSource(item *summaryResponseItem, validator model.SourceURLValidator) (string, bool) {
	normalizedURL := strings.TrimSpace(item.SourceURL)

	if validator == nil {
		return normalizedURL, true
	}

	tier, sourceURL, err := validator.ValidateSourceURL(item.SourceURL)
	if err != nil {
		return "", false
	}

	if tier == model.SourceTierCommunity && !validator.HasCorroboration(item.Summary) {
		return "", false
	}

	return sourceURL, true
}

// categoryLabel: Category를 한국어 레이블로 변환.
func categoryLabel(cat model.Category) string {
	if label, ok := categoryLabels[cat]; ok {
		return label
	}

	return "기타"
}

func BuildDeterministicFallback(period model.Period, candidates []model.FilteredCandidate) *model.Digest {
	items := make([]model.SummaryItem, 0, min(5, len(candidates)))
	for idx := range candidates {
		if idx >= 5 {
			break
		}

		candidate := &candidates[idx]

		localTime := candidate.EffectiveDate.In(kst)
		dateText := fmt.Sprintf("%d/%d(%s)", localTime.Month(), localTime.Day(), sharedmodel.WeekdayKR[localTime.Weekday()])
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
	if category, ok := normalizedCategories[normalized]; ok {
		return category
	}

	return model.CategoryOther
}
