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
	"strings"
	"sync"
	"time"

	json "github.com/park285/shared-go/pkg/json"

	sharedmodel "github.com/kapu/hololive-api/internal/planes/llm/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const summaryCacheTTL = 24 * time.Hour
const searchTimeout = 12 * time.Second

type CacheStore interface {
	Get(ctx context.Context, key string, dest any) error
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
}

type EventSummarizer struct {
	llm      LLMClient // nil이면 비활성
	cache    CacheStore
	searcher sharedmodel.WebSearcher // nil 허용
	reviewer LLMClient               // nil이면 consensus stage2 비활성
	// nil이면 stage3(adjudication) 비활성
	adjudicator LLMClient
	consensus   SummarizerConsensusConfig
	logger      *slog.Logger
}

type SummaryResult struct {
	Text       string
	ResultType sharedmodel.SummaryResultType
}

type SummarizerConsensusConfig struct {
	Enabled             bool
	ConfidenceThreshold float64
	ReviewTimeout       time.Duration
	AdjudicateTimeout   time.Duration
}

type SummarizerOption func(*EventSummarizer)

func WithSummarizerConsensus(reviewer, adjudicator LLMClient, config SummarizerConsensusConfig) SummarizerOption {
	return func(s *EventSummarizer) {
		s.reviewer = reviewer
		s.adjudicator = adjudicator
		s.consensus = normalizeConsensusConfig(config)
	}
}

func normalizeConsensusConfig(config SummarizerConsensusConfig) SummarizerConsensusConfig {
	// ConfidenceThreshold는 config 레이어(clampConfidence)에서 이미 보장됨
	if config.ReviewTimeout < 5*time.Second {
		config.ReviewTimeout = 30 * time.Second
	}
	if config.AdjudicateTimeout < 5*time.Second {
		config.AdjudicateTimeout = 45 * time.Second
	}
	return config
}

// llm이 nil이면 Summarize()는 항상 빈 문자열을 반환합니다.
func NewEventSummarizer(llm LLMClient, cache CacheStore, searcher sharedmodel.WebSearcher, logger *slog.Logger, opts ...SummarizerOption) *EventSummarizer {
	s := &EventSummarizer{
		llm:      llm,
		cache:    cache,
		searcher: searcher,
		logger:   logger,
		consensus: normalizeConsensusConfig(SummarizerConsensusConfig{
			Enabled:             false,
			ConfidenceThreshold: 0.85,
			ReviewTimeout:       30 * time.Second,
			AdjudicateTimeout:   45 * time.Second,
		}),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// LLM 비활성 또는 실패 시 빈 문자열을 반환합니다 (호출부에서 fallback 처리).
func (s *EventSummarizer) Summarize(ctx context.Context, events []domain.MajorEvent, summaryType SummaryType, periodKey string) string {
	return s.SummarizeResult(ctx, events, summaryType, periodKey).Text
}

func (s *EventSummarizer) SummarizeResult(ctx context.Context, events []domain.MajorEvent, summaryType SummaryType, periodKey string) SummaryResult {
	if s.llm == nil || len(events) == 0 {
		return SummaryResult{ResultType: sharedmodel.SummaryResultEmpty}
	}

	cacheKey := s.summaryCacheKey(events, summaryType, periodKey)
	if cached, ok := s.cachedSummaryResult(ctx, cacheKey, summaryType, periodKey); ok {
		return cached
	}

	searchContext := s.runDualSearch(ctx, summaryType, periodKey)
	resp, err := s.buildSummaryResponse(ctx, events, summaryType, periodKey, searchContext)
	if err != nil {
		s.logger.Error("LLM 요약 실패 (fallback 사용)",
			slog.String("type", string(summaryType)),
			slog.String("error", err.Error()))
		return SummaryResult{ResultType: sharedmodel.SummaryResultEmpty}
	}

	resp.DiscoveredEvents = filterTrustedDiscoveredEvents(resp.DiscoveredEvents)
	resp = s.applyConsensusReview(ctx, events, summaryType, periodKey, searchContext, resp)

	result := assembleSummaryText(resp)
	if result == "" {
		s.logger.Warn("LLM 요약 결과가 비어있음",
			slog.String("type", string(summaryType)))
		return SummaryResult{ResultType: sharedmodel.SummaryResultEmpty}
	}

	result = s.reviewFinalSummaryOutput(ctx, events, summaryType, periodKey, resp, result)
	s.storeSummaryResult(ctx, cacheKey, result)
	s.logSummaryResultGenerated(summaryType, events, resp, result)

	return SummaryResult{
		Text:       result,
		ResultType: sharedmodel.SummaryResultPrimary,
	}
}

func (s *EventSummarizer) summaryCacheKey(events []domain.MajorEvent, summaryType SummaryType, periodKey string) string {
	cacheKey, err := buildSummaryCacheKey(events, summaryType, periodKey)
	if err != nil {
		s.logger.Warn("LLM 요약 캐시 키 생성 실패", slog.String("error", err.Error()))
	}
	return cacheKey
}

func (s *EventSummarizer) cachedSummaryResult(ctx context.Context, cacheKey string, summaryType SummaryType, periodKey string) (SummaryResult, bool) {
	if s.cache == nil || cacheKey == "" {
		return SummaryResult{}, false
	}
	var cached string
	if err := s.cache.Get(ctx, cacheKey, &cached); err != nil || cached == "" {
		return SummaryResult{}, false
	}
	s.logger.Info("LLM 요약 캐시 히트",
		slog.String("type", string(summaryType)),
		slog.String("period", periodKey))
	return SummaryResult{Text: cached, ResultType: sharedmodel.SummaryResultPrimary}, true
}

func (s *EventSummarizer) applyConsensusReview(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey string,
	searchContext string,
	resp *summaryResponse,
) *summaryResponse {
	if !s.consensus.Enabled || !shouldRunConsensusReview(resp) {
		return resp
	}
	consensusResp, consensusUsed := s.runConsensus(ctx, events, summaryType, periodKey, searchContext, resp)
	if consensusResp == nil {
		return resp
	}
	if consensusUsed {
		consensusResp.DiscoveredEvents = filterTrustedDiscoveredEvents(consensusResp.DiscoveredEvents)
	}
	return consensusResp
}

func (s *EventSummarizer) reviewFinalSummaryOutput(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey string,
	resp *summaryResponse,
	result string,
) string {
	if s.reviewer == nil || !shouldRunFinalOutputReview(resp, result) {
		return result
	}
	reviewed, applied := s.runFinalOutputReview(ctx, events, summaryType, periodKey, result)
	if !applied {
		return result
	}
	return reviewed
}

func (s *EventSummarizer) storeSummaryResult(ctx context.Context, cacheKey, result string) {
	if s.cache == nil || cacheKey == "" {
		return
	}
	if err := s.cache.Set(ctx, cacheKey, result, summaryCacheTTL); err != nil {
		s.logger.Warn("LLM 요약 캐시 저장 실패", slog.String("error", err.Error()))
	}
}

func (s *EventSummarizer) logSummaryResultGenerated(summaryType SummaryType, events []domain.MajorEvent, resp *summaryResponse, result string) {
	s.logger.Info("LLM 요약 생성 완료",
		slog.String("type", string(summaryType)),
		slog.Int("event_count", len(events)),
		slog.Int("highlight_count", len(resp.Highlights)),
		slog.Int("discovered_count", len(resp.DiscoveredEvents)),
		slog.Int("summary_length", len(result)))
}

func shouldRunConsensusReview(resp *summaryResponse) bool {
	if resp == nil {
		return false
	}
	return len(resp.Highlights) > 0 || len(resp.OngoingEvents) > 0 || len(resp.DiscoveredEvents) > 0
}

func shouldRunFinalOutputReview(resp *summaryResponse, assembled string) bool {
	if resp == nil || strings.TrimSpace(assembled) == "" {
		return false
	}
	if len(resp.OngoingEvents) > 0 || len(resp.DiscoveredEvents) > 0 {
		return true
	}
	return len(resp.Highlights) > 0
}

func (s *EventSummarizer) searchWithTimeout(ctx context.Context, query, warnMessage string) ([]sharedmodel.SearchResult, bool) {
	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	results, err := s.searcher.Search(searchCtx, query)
	if err != nil {
		s.logger.Warn(warnMessage, slog.String("error", err.Error()))
		return nil, false
	}

	return results, true
}

// runDualSearch: 1차 범용 + 2차 KR 파트너 검색을 병렬 실행하고 병합된 결과를 포맷팅합니다.
func (s *EventSummarizer) runDualSearch(ctx context.Context, summaryType SummaryType, periodKey string) string {
	if s.searcher == nil {
		return ""
	}

	var (
		results   []sharedmodel.SearchResult
		krResults []sharedmodel.SearchResult
		mu        sync.Mutex
		wg        sync.WaitGroup
	)

	runSearch := func(query string, warnMessage string, dst *[]sharedmodel.SearchResult) {
		wg.Go(func() {
			found, ok := s.searchWithTimeout(ctx, query, warnMessage)
			if !ok {
				return
			}
			mu.Lock()
			*dst = found
			mu.Unlock()
		})
	}

	runSearch(buildSearchQuery(summaryType, periodKey), "Exa 1차 검색 실패 (graceful degradation)", &results)
	runSearch(buildKRPartnerSearchQuery(periodKey), "Exa KR 2차 검색 실패 (graceful degradation)", &krResults)

	wg.Wait()

	// 병합 파이프라인: append → dedupe → cap
	combined := make([]sharedmodel.SearchResult, 0, len(results)+len(krResults))
	combined = append(combined, results...)
	combined = append(combined, krResults...)
	deduped := dedupeSearchResults(combined)
	capped := capSearchResults(deduped, maxSearchResults)

	if len(capped) > 0 {
		s.logger.Info("Exa pre-search 완료",
			slog.Int("primary_count", len(results)),
			slog.Int("kr_count", len(krResults)),
			slog.Int("final_count", len(capped)))
		return formatSearchResults(capped)
	}
	return ""
}

func (s *EventSummarizer) buildSummaryResponse(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, searchContext string,
) (*summaryResponse, error) {
	sysPrompt, err := getSystemPrompt(summaryType)
	if err != nil {
		return nil, fmt.Errorf("get system prompt: %w", err)
	}
	userPrompt := buildUserPrompt(events, summaryType, periodKey, searchContext)
	schema := summaryResponseSchema()

	rawJSON, err := s.llm.GenerateJSON(ctx, sysPrompt, userPrompt, schema)
	if err != nil {
		return nil, fmt.Errorf("generate summary json: %w", err)
	}

	var resp summaryResponse
	if err := json.Unmarshal([]byte(rawJSON), &resp); err != nil {
		return nil, fmt.Errorf("parse summary json: %w", err)
	}
	return &resp, nil
}

func filterTrustedDiscoveredEvents(input []discoveredEvent) []discoveredEvent {
	if len(input) == 0 {
		return input
	}

	filtered := make([]discoveredEvent, 0, len(input))
	for i := range input {
		item := input[i]
		if isTrustedDiscoveredSource(item.Source) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
