package summarizer

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const summaryCacheTTL = 24 * time.Hour
const searchTimeout = 12 * time.Second

// CacheStore: 요약 캐시 저장소 인터페이스
type CacheStore interface {
	Get(ctx context.Context, key string, dest any) error
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
}

// EventSummarizer: LLM 기반 이벤트 요약 서비스
type EventSummarizer struct {
	llm      LLMClient // nil이면 비활성
	cache    CacheStore
	searcher WebSearcher // nil 허용
	reviewer LLMClient   // nil이면 consensus stage2 비활성
	// nil이면 stage3(adjudication) 비활성
	adjudicator LLMClient
	consensus   SummarizerConsensusConfig
	logger      *slog.Logger
}

type SummarizerConsensusConfig struct {
	Enabled             bool
	ConfidenceThreshold float64
	ReviewTimeout       time.Duration
	AdjudicateTimeout   time.Duration
}

type SummarizerOption func(*EventSummarizer)

func WithSummarizerConsensus(reviewer, adjudicator LLMClient, cfg SummarizerConsensusConfig) SummarizerOption {
	return func(s *EventSummarizer) {
		if s == nil {
			return
		}
		s.reviewer = reviewer
		s.adjudicator = adjudicator
		s.consensus = normalizeConsensusConfig(cfg)
	}
}

func normalizeConsensusConfig(cfg SummarizerConsensusConfig) SummarizerConsensusConfig {
	// ConfidenceThreshold는 config 레이어(clampConfidence)에서 이미 보장됨
	if cfg.ReviewTimeout < 5*time.Second {
		cfg.ReviewTimeout = 30 * time.Second
	}
	if cfg.AdjudicateTimeout < 5*time.Second {
		cfg.AdjudicateTimeout = 45 * time.Second
	}
	return cfg
}

// NewEventSummarizer: 이벤트 요약 서비스를 생성합니다.
// llm이 nil이면 Summarize()는 항상 빈 문자열을 반환합니다.
func NewEventSummarizer(llm LLMClient, cache CacheStore, searcher WebSearcher, logger *slog.Logger, opts ...SummarizerOption) *EventSummarizer {
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

// Summarize: 이벤트 목록을 LLM 구조화 출력으로 요약합니다.
// LLM 비활성 또는 실패 시 빈 문자열을 반환합니다 (호출부에서 fallback 처리).
func (s *EventSummarizer) Summarize(ctx context.Context, events []domain.MajorEvent, summaryType SummaryType, periodKey string) string {
	if s == nil || s.llm == nil || len(events) == 0 {
		return ""
	}

	cacheKey := fmt.Sprintf("majorevent:summary:%s:%s:%s", promptVersion, summaryType, periodKey)

	// 캐시 조회
	if s.cache != nil {
		var cached string
		if err := s.cache.Get(ctx, cacheKey, &cached); err == nil && cached != "" {
			s.logger.Info("LLM 요약 캐시 히트",
				slog.String("type", string(summaryType)),
				slog.String("period", periodKey))
			return cached
		}
	}

	searchContext := s.runDualSearch(ctx, summaryType, periodKey)

	resp, err := s.buildSummaryResponse(ctx, events, summaryType, periodKey, searchContext)
	if err != nil {
		s.logger.Error("LLM 요약 실패 (fallback 사용)",
			slog.String("type", string(summaryType)),
			slog.String("error", err.Error()))
		return ""
	}

	// post validation: trusted source 기반 discovered_events 정리
	resp.DiscoveredEvents = filterTrustedDiscoveredEvents(resp.DiscoveredEvents)

	// consensus: primary -> reviewer -> adjudicator(조건부)
	if s.consensus.Enabled && s.reviewer != nil {
		consensusResp, consensusUsed := s.runConsensus(ctx, events, summaryType, periodKey, searchContext, resp)
		if consensusResp != nil {
			resp = consensusResp
			if consensusUsed {
				resp.DiscoveredEvents = filterTrustedDiscoveredEvents(resp.DiscoveredEvents)
			}
		}
	}

	result := assembleSummaryText(resp)
	if result == "" {
		s.logger.Warn("LLM 요약 결과가 비어있음",
			slog.String("type", string(summaryType)))
		return ""
	}

	// 최종 출력 취합 리뷰: 리뷰어가 있으면 텍스트 레벨 중복/형식 점검 후 보정
	if s.reviewer != nil {
		if reviewed, applied := s.runFinalOutputReview(ctx, events, summaryType, periodKey, result); applied {
			result = reviewed
		}
	}

	// 캐시 저장
	if s.cache != nil {
		if err := s.cache.Set(ctx, cacheKey, result, summaryCacheTTL); err != nil {
			s.logger.Warn("LLM 요약 캐시 저장 실패", slog.String("error", err.Error()))
		}
	}

	s.logger.Info("LLM 요약 생성 완료",
		slog.String("type", string(summaryType)),
		slog.Int("event_count", len(events)),
		slog.Int("highlight_count", len(resp.Highlights)),
		slog.Int("discovered_count", len(resp.DiscoveredEvents)),
		slog.Int("summary_length", len(result)))

	return result
}

// runDualSearch: 1차 범용 + 2차 KR 파트너 검색을 병렬 실행하고 병합된 결과를 포맷팅합니다.
func (s *EventSummarizer) runDualSearch(ctx context.Context, summaryType SummaryType, periodKey string) string {
	if s.searcher == nil {
		return ""
	}

	var (
		results   []SearchResult
		krResults []SearchResult
		mu        sync.Mutex
		wg        sync.WaitGroup
	)

	// 1차: 범용 Exa 검색
	wg.Go(func() {
		searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
		defer cancel()
		r, err := s.searcher.Search(searchCtx, buildSearchQuery(summaryType, periodKey))
		if err != nil {
			s.logger.Warn("Exa 1차 검색 실패 (graceful degradation)", slog.String("error", err.Error()))
			return
		}
		mu.Lock()
		results = r
		mu.Unlock()
	})

	// 2차: 한국 파트너 전용 검색
	wg.Go(func() {
		searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
		defer cancel()
		r, err := s.searcher.Search(searchCtx, buildKRPartnerSearchQuery(periodKey))
		if err != nil {
			s.logger.Warn("Exa KR 2차 검색 실패 (graceful degradation)", slog.String("error", err.Error()))
			return
		}
		mu.Lock()
		krResults = r
		mu.Unlock()
	})

	wg.Wait()

	// 병합 파이프라인: append → dedupe → cap
	combined := make([]SearchResult, 0, len(results)+len(krResults))
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
	sysPrompt := getSystemPrompt(summaryType)
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

func isTrustedDiscoveredSource(source string) bool {
	normalized := strings.ToLower(strings.TrimSpace(source))
	if normalized == "" {
		return false
	}

	// URL 형식인 경우만 URL 검증 경로 진입 — bare domain의 parseSourceURL auto-prepend 우회 차단
	if strings.HasPrefix(normalized, "http://") || strings.HasPrefix(normalized, "https://") {
		if trusted, handled := isTrustedURLSource(normalized); handled {
			return trusted
		}
	}

	return isTrustedTextSource(normalized)
}

func isTrustedURLSource(source string) (trusted bool, handled bool) {
	parsed, err := parseSourceURL(source)
	if err != nil || parsed == nil || parsed.Hostname() == "" {
		return false, false
	}

	host := normalizeHost(parsed.Hostname())
	if host == "" {
		return false, true
	}

	if isTrustedDomainHost(host) {
		return true, true
	}
	if !isSocialHost(host) {
		return false, true
	}

	account := extractSocialAccount(parsed.Path)
	if account == "" {
		return false, true
	}
	for _, trustedAccount := range constants.MajorEventConfig.TrustedSocialAccounts {
		if account == strings.ToLower(strings.TrimSpace(trustedAccount)) {
			return true, true
		}
	}
	return false, true
}

func isTrustedTextSource(source string) bool {
	for _, domain := range constants.MajorEventConfig.TrustedSourceDomains {
		token := normalizeHost(domain)
		if token == "" {
			continue
		}
		if source == "https://"+token || source == "http://"+token {
			return true
		}
	}
	for _, account := range constants.MajorEventConfig.TrustedSocialAccounts {
		token := strings.ToLower(strings.TrimSpace(account))
		if token == "" {
			continue
		}
		if source == "@"+token ||
			source == "x.com/"+token ||
			source == "twitter.com/"+token ||
			source == "https://x.com/"+token ||
			source == "https://twitter.com/"+token ||
			source == "http://x.com/"+token ||
			source == "http://twitter.com/"+token {
			return true
		}
	}
	return false
}

func parseSourceURL(raw string) (*url.URL, error) {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse source url: %w", err)
		}
		return parsed, nil
	}
	parsed, err := url.Parse("https://" + raw)
	if err != nil {
		return nil, fmt.Errorf("parse source url with default scheme: %w", err)
	}
	return parsed, nil
}

func normalizeHost(host string) string {
	normalized := strings.ToLower(strings.TrimSpace(host))
	normalized = strings.TrimPrefix(normalized, "www.")
	return normalized
}

func isTrustedDomainHost(host string) bool {
	for _, domain := range constants.MajorEventConfig.TrustedSourceDomains {
		token := normalizeHost(domain)
		if token == "" {
			continue
		}
		if host == token || strings.HasSuffix(host, "."+token) {
			return true
		}
	}
	return false
}

func isSocialHost(host string) bool {
	return host == "x.com" || host == "twitter.com"
}

func extractSocialAccount(path string) string {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return ""
	}
	account := strings.TrimPrefix(parts[0], "@")
	return strings.ToLower(strings.TrimSpace(account))
}
