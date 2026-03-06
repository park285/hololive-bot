package holodex

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/fallback"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
	"github.com/kapu/hololive-shared/pkg/util"
)

const searchChannelsCacheKeyPrefix = "search_channels:"

// ErrInvalidStreamOrg: 지원하지 않는 org 파라미터가 전달될 때 반환됩니다.
var ErrInvalidStreamOrg = stdErrors.New("invalid stream org parameter")

var _ domain.StreamProvider = (*Service)(nil)

// ChannelRaw: Holodex API로부터 수신한 채널 정보의 Raw 데이터 구조체
type ChannelRaw struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	EnglishName     *string `json:"english_name,omitempty"`
	Photo           *string `json:"photo,omitempty"`
	Twitter         *string `json:"twitter,omitempty"`
	VideoCount      *int    `json:"video_count,omitempty"`
	SubscriberCount *int    `json:"subscriber_count,omitempty"`
	Org             *string `json:"org,omitempty"`
	Suborg          *string `json:"suborg,omitempty"`
	Group           *string `json:"group,omitempty"`
}

// StreamRaw: Holodex API로부터 수신한 방송(스트림) 정보의 Raw 데이터 구조체
type StreamRaw struct {
	ID             string              `json:"id"`
	Title          string              `json:"title"`
	ChannelID      *string             `json:"channel_id,omitempty"`
	Status         domain.StreamStatus `json:"status"`
	StartScheduled *string             `json:"start_scheduled,omitempty"`
	StartActual    *string             `json:"start_actual,omitempty"`
	Duration       *int                `json:"duration,omitempty"`
	Link           *string             `json:"link,omitempty"`
	Thumbnail      *string             `json:"thumbnail,omitempty"`
	TopicID        *string             `json:"topic_id,omitempty"`
	LiveViewers    *int                `json:"live_viewers,omitempty"`
	Channel        *ChannelRaw         `json:"channel,omitempty"`
}

// Service: Holodex External API와 통신하여 채널 및 스트림 정보를 가져오는 클라이언트 서비스
// 캐싱 및 스크래핑 폴백(Fallback) 기능을 포함한다.
type Service struct {
	requester    Requester
	scraper      *ScraperService
	logger       *slog.Logger
	cacheManager *CacheManager
	mapper       *StreamMapper
	filter       *StreamFilter
	retry        *retryScheduler
}

// NewHolodexService: 새로운 Holodex API 서비스 인스턴스를 생성한다. (API Key 검증 포함)
func NewHolodexService(baseURL string, apiKeys []string, cacheSvc cache.Client, scraperSvc *ScraperService, logger *slog.Logger) (*Service, error) {
	if len(apiKeys) == 0 {
		return nil, fmt.Errorf("at least one Holodex API key is required")
	}

	apiKey := apiKeys[0]
	logger.Info("Holodex API key configured")

	// DefaultTransport 복제: TCP Keep-Alive(30s), TLSHandshakeTimeout(10s), Proxy 지원 등 안전장치 유지
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = constants.HolodexTransportConfig.MaxConnsPerHost
	transport.MaxIdleConnsPerHost = constants.HolodexTransportConfig.MaxIdleConnsPerHost
	transport.IdleConnTimeout = constants.HolodexTransportConfig.IdleConnTimeout
	// HTTP/2 활성화 유지 (DefaultTransport 기본값): Cloudflare가 HTTP/2 응답을 보내므로 필수

	httpClient := &http.Client{
		Timeout:   constants.APIConfig.HolodexTimeout,
		Transport: transport,
	}

	var distributedLimiter *ratelimit.SlidingWindowLimiter
	if constants.HolodexDistributedRateLimitConfig.Enabled {
		var err error
		distributedLimiter, err = ratelimit.NewSlidingWindowLimiter(
			cacheSvc,
			constants.HolodexDistributedRateLimitConfig.KeyPrefix,
			logger,
		)
		if err != nil {
			return nil, fmt.Errorf("initialize holodex distributed rate limiter: %w", err)
		}
	}

	requester := NewHolodexAPIClient(httpClient, baseURL, apiKey, logger, distributedLimiter)

	svc := &Service{
		requester:    requester,
		scraper:      scraperSvc,
		logger:       logger,
		cacheManager: NewCacheManager(cacheSvc, logger),
		mapper:       NewStreamMapper(logger),
		filter:       NewStreamFilter(logger),
	}
	svc.retry = newRetryScheduler(
		constants.RetrySchedulerConfig.Delay,
		constants.RetrySchedulerConfig.Timeout,
		constants.RetrySchedulerConfig.MaxSize,
		logger,
	)
	return svc, nil
}

// SetScraperProxyEnabled: Holodex fallback 스크래퍼의 프록시 사용 여부를 런타임에 토글합니다.
func (h *Service) SetScraperProxyEnabled(enabled bool) bool {
	if h.scraper == nil {
		return false
	}
	return h.scraper.SetYouTubeProxyEnabled(enabled)
}

// ScraperProxyEnabled: Holodex fallback 스크래퍼의 현재 프록시 활성 상태를 반환합니다.
func (h *Service) ScraperProxyEnabled() bool {
	if h.scraper == nil {
		return false
	}
	return h.scraper.YouTubeProxyEnabled()
}

// Stop: 서비스 리소스를 정리합니다.
func (h *Service) Stop() {
	if h.retry != nil {
		h.retry.stop()
	}
}

// scheduleRetryIfNeeded: 재시도가 필요한 경우 지연 재시도를 등록합니다.
// retry는 지연 실행(35s)이므로 원래 context를 전파하지 않고, execute()에서 독립 context를 생성합니다.
//
//nolint:contextcheck // retry callback은 지연 실행되므로 원래 ctx 대신 독립 context 사용
func (h *Service) scheduleRetryIfNeeded(ctx context.Context, key string, fn func(ctx context.Context)) {
	if h.retry == nil || isRetryContext(ctx) || ctx.Err() != nil {
		return
	}
	h.retry.schedule(key, fn)
}

// SupportedStreamOrgParams: stream 조회 API에서 허용하는 org 파라미터 목록을 반환합니다.
func SupportedStreamOrgParams() []string {
	return []string{
		strings.ToLower(constants.HolodexAPIParams.OrgHololive),
		strings.ToLower(constants.HolodexAPIParams.OrgVSpo),
		strings.ToLower(constants.HolodexAPIParams.OrgStellive),
		strings.ToLower(constants.HolodexAPIParams.OrgIndie),
		constants.HolodexAPIParams.OrgAll,
	}
}

// GetLiveStreams: 기본 org(Hololive)의 현재 진행 중인 VTuber 스트림 목록을 조회합니다.
func (h *Service) GetLiveStreams(ctx context.Context) ([]*domain.Stream, error) {
	return h.GetLiveStreamsByOrg(ctx, constants.HolodexAPIParams.OrgHololive)
}

// GetLiveStreamsByOrg: org별 현재 진행 중인 VTuber 스트림 목록을 조회합니다.
// org 미지정 시 Hololive를 기본값으로 사용합니다.
func (h *Service) GetLiveStreamsByOrg(ctx context.Context, org string) ([]*domain.Stream, error) {
	resolvedOrg, err := resolveStreamOrg(org)
	if err != nil {
		return nil, err
	}

	if cached, found := h.cacheManager.GetLiveStreamsByOrg(ctx, resolvedOrg); found {
		return cached, nil
	}

	var allStreams []*domain.Stream
	seen := make(map[string]bool)

	targetOrgs := streamTargetOrgs(resolvedOrg)
	primary := fallback.RunPrimary(ctx, targetOrgs, fallback.FetchPlan[string, struct{}]{Parallelism: 1}, func(fetchCtx context.Context, targetOrg string) error {
		streams, fetchErr := h.fetchStreamsByOrg(fetchCtx, targetOrg, constants.HolodexAPIParams.StatusLive, 0)
		if fetchErr != nil {
			h.logger.Warn("Failed to get live streams for org",
				slog.String("org", targetOrg), slog.Any("error", fetchErr))
			return fetchErr
		}

		filtered := h.filter.FilterHololiveStreams(streams)
		filtered = filterStreamsByRequestedOrg(filtered, resolvedOrg)
		liveOnly := filterStreamsByStatus(filtered, domain.StreamStatusLive)

		for _, s := range liveOnly {
			if !seen[s.ID] {
				seen[s.ID] = true
				allStreams = append(allStreams, s)
			}
		}
		return nil
	})
	fallback.ObservePrimaryPhase("holodex", "live_streams", len(targetOrgs), primary.Succeeded, len(primary.Failed))

	if primary.HasFailures() {
		h.scheduleRetryIfNeeded(ctx, fmt.Sprintf("live_streams_%s", strings.ToLower(resolvedOrg)), func(retryCtx context.Context) {
			_, _ = h.GetLiveStreamsByOrg(retryCtx, resolvedOrg)
		})
	}

	// Hololive 전용 스크래퍼 폴백
	scraperFallbackPolicy := fallback.Policy{Trigger: fallback.TriggerOnEmptyPrimaryWithError}
	secondary, err := fallback.RunSecondary(ctx, fallback.SecondaryPlan{
		Service:   "holodex",
		Operation: "live_streams",
		Trigger:   scraperFallbackPolicy.Trigger,
		ShouldRun: h.scraper != nil && supportsScraperFallback(resolvedOrg) &&
			scraperFallbackPolicy.ShouldRun(len(allStreams), len(primary.Failed)),
		Run: func(runCtx context.Context) (fallback.SecondaryResult, error) {
			h.logger.Warn("Primary org fetch returned no live streams, using scraper fallback",
				slog.Int("failed_orgs", len(primary.Failed)))
			scraperStreams, scraperErr := h.scraper.FetchAllStreams(runCtx)
			if scraperErr != nil {
				return fallback.SecondaryResult{}, scraperErr
			}
			liveStreams := filterStreamsByStatus(scraperStreams, domain.StreamStatusLive)
			h.cacheManager.SetLiveStreamsByOrg(runCtx, resolvedOrg, liveStreams)
			allStreams = liveStreams
			return fallback.SecondaryResult{
				Items:     len(liveStreams),
				Successes: 1,
			}, nil
		},
	})
	if err == nil && secondary.Outcome == "hit" {
		return allStreams, nil
	}

	h.cacheManager.SetLiveStreamsByOrg(ctx, resolvedOrg, allStreams)
	return allStreams, nil
}

// GetUpcomingStreams: 기본 org(Hololive)의 예정 스트림 목록을 조회합니다.
func (h *Service) GetUpcomingStreams(ctx context.Context, hours int) ([]*domain.Stream, error) {
	return h.GetUpcomingStreamsByOrg(ctx, hours, constants.HolodexAPIParams.OrgHololive)
}

// GetUpcomingStreamsByOrg: org별 예정 스트림 목록을 조회합니다.
// org 미지정 시 Hololive를 기본값으로 사용합니다.
func (h *Service) GetUpcomingStreamsByOrg(ctx context.Context, hours int, org string) ([]*domain.Stream, error) {
	resolvedOrg, err := resolveStreamOrg(org)
	if err != nil {
		return nil, err
	}

	if cached, found := h.cacheManager.GetUpcomingStreamsByOrg(ctx, resolvedOrg, hours); found {
		return cached, nil
	}

	var allStreams []*domain.Stream
	seen := make(map[string]bool)

	targetOrgs := streamTargetOrgs(resolvedOrg)
	primary := fallback.RunPrimary(ctx, targetOrgs, fallback.FetchPlan[string, struct{}]{Parallelism: 1}, func(fetchCtx context.Context, targetOrg string) error {
		streams, fetchErr := h.fetchStreamsByOrg(fetchCtx, targetOrg, constants.HolodexAPIParams.StatusUpcoming, hours)
		if fetchErr != nil {
			h.logger.Warn("Failed to get upcoming streams for org",
				slog.String("org", targetOrg), slog.Any("error", fetchErr))
			return fetchErr
		}

		filtered := h.filter.FilterHololiveStreams(streams)
		filtered = filterStreamsByRequestedOrg(filtered, resolvedOrg)
		upcoming := h.filter.FilterUpcomingStreams(filtered)

		for _, s := range upcoming {
			if !seen[s.ID] {
				seen[s.ID] = true
				allStreams = append(allStreams, s)
			}
		}
		return nil
	})
	fallback.ObservePrimaryPhase("holodex", "upcoming_streams", len(targetOrgs), primary.Succeeded, len(primary.Failed))

	if primary.HasFailures() {
		h.scheduleRetryIfNeeded(ctx, fmt.Sprintf("upcoming_%s_%d", strings.ToLower(resolvedOrg), hours), func(retryCtx context.Context) {
			_, _ = h.GetUpcomingStreamsByOrg(retryCtx, hours, resolvedOrg)
		})
	}

	// Hololive 전용 스크래퍼 폴백
	scraperFallbackPolicy := fallback.Policy{Trigger: fallback.TriggerOnEmptyPrimaryWithError}
	secondary, err := fallback.RunSecondary(ctx, fallback.SecondaryPlan{
		Service:   "holodex",
		Operation: "upcoming_streams",
		Trigger:   scraperFallbackPolicy.Trigger,
		ShouldRun: h.scraper != nil && supportsScraperFallback(resolvedOrg) &&
			scraperFallbackPolicy.ShouldRun(len(allStreams), len(primary.Failed)),
		Run: func(runCtx context.Context) (fallback.SecondaryResult, error) {
			h.logger.Warn("Primary org fetch returned no upcoming streams, using scraper fallback",
				slog.Int("failed_orgs", len(primary.Failed)))
			scraperStreams, scraperErr := h.scraper.FetchAllStreams(runCtx)
			if scraperErr != nil {
				return fallback.SecondaryResult{}, scraperErr
			}
			upcomingStreams := h.filter.FilterUpcomingStreams(scraperStreams)
			h.cacheManager.SetUpcomingStreamsByOrg(runCtx, resolvedOrg, hours, upcomingStreams)
			allStreams = upcomingStreams
			return fallback.SecondaryResult{
				Items:     len(upcomingStreams),
				Successes: 1,
			}, nil
		},
	})
	if err == nil && secondary.Outcome == "hit" {
		return allStreams, nil
	}

	h.cacheManager.SetUpcomingStreamsByOrg(ctx, resolvedOrg, hours, allStreams)
	return allStreams, nil
}

func (h *Service) fetchStreamsByOrg(ctx context.Context, org, status string, hours int) ([]*domain.Stream, error) {
	if org == constants.HolodexAPIParams.OrgIndie {
		streams, err := h.fetchIndieStreams(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch indie streams: %w", err)
		}
		return streams, nil
	}

	params := url.Values{}
	params.Set("org", org)
	params.Set("status", status)
	params.Set("type", constants.HolodexAPIParams.TypeStream)
	if status == constants.HolodexAPIParams.StatusUpcoming {
		params.Set("max_upcoming_hours", fmt.Sprintf("%d", util.Min(hours, constants.HolodexAPIParams.MaxUpcomingHours)))
		params.Set("order", "asc")
		params.Set("sort", "start_scheduled")
	}

	body, err := h.requester.DoRequest(ctx, "GET", "/live", params)
	if err != nil {
		return nil, fmt.Errorf("get streams by org (%s): %w", org, err)
	}

	var rawStreams []StreamRaw
	if err := json.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("unmarshal streams by org (%s): %w", org, err)
	}

	return h.mapper.MapStreamsResponse(rawStreams), nil
}

func resolveStreamOrg(org string) (string, error) {
	switch normalizeStreamOrg(org) {
	case "", "holo", strings.ToLower(constants.HolodexAPIParams.OrgHololive):
		return constants.HolodexAPIParams.OrgHololive, nil
	case strings.ToLower(constants.HolodexAPIParams.OrgVSpo):
		return constants.HolodexAPIParams.OrgVSpo, nil
	case strings.ToLower(constants.HolodexAPIParams.OrgStellive):
		return constants.HolodexAPIParams.OrgStellive, nil
	case strings.ToLower(constants.HolodexAPIParams.OrgIndie):
		return constants.HolodexAPIParams.OrgIndie, nil
	case constants.HolodexAPIParams.OrgAll:
		return constants.HolodexAPIParams.OrgAll, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrInvalidStreamOrg, stringutil.TrimSpace(org))
	}
}

func streamTargetOrgs(org string) []string {
	if org != constants.HolodexAPIParams.OrgAll {
		return []string{org}
	}

	targets := make([]string, 0, len(constants.HolodexAPIParams.SyncTargetOrgs)+1)
	targets = append(targets, constants.HolodexAPIParams.SyncTargetOrgs...)
	targets = append(targets, constants.HolodexAPIParams.OrgIndie)
	return targets
}

func filterStreamsByRequestedOrg(streams []*domain.Stream, org string) []*domain.Stream {
	if org == constants.HolodexAPIParams.OrgAll {
		return streams
	}

	target := normalizeStreamOrg(org)
	filtered := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if stream.Channel == nil || stream.Channel.Org == nil {
			continue
		}
		if normalizeStreamOrg(*stream.Channel.Org) == target {
			filtered = append(filtered, stream)
		}
	}
	return filtered
}

func filterStreamsByStatus(streams []*domain.Stream, status domain.StreamStatus) []*domain.Stream {
	filtered := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if stream.Status == status {
			filtered = append(filtered, stream)
		}
	}
	return filtered
}

func supportsScraperFallback(org string) bool {
	return org == constants.HolodexAPIParams.OrgHololive
}

func normalizeStreamOrg(org string) string {
	normalized := strings.ToLower(stringutil.TrimSpace(org))
	return strings.TrimSuffix(normalized, "!")
}

// GetChannelSchedule: 특정 채널의 방송 일정(예정된 방송)을 조회합니다.
// includeLive가 true이면 현재 진행 중인 방송도 포함한다.
func (h *Service) GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*domain.Stream, error) {
	if cached, found := h.cacheManager.GetChannelSchedule(ctx, channelID, hours, includeLive); found {
		copied := make([]*domain.Stream, len(cached))
		for i, stream := range cached {
			streamCopy := *stream
			if stream.StartScheduled != nil {
				t := *stream.StartScheduled
				streamCopy.StartScheduled = &t
			}
			if stream.StartActual != nil {
				t := *stream.StartActual
				streamCopy.StartActual = &t
			}
			copied[i] = &streamCopy
		}

		if includeLive {
			return copied, nil
		}
		return h.filter.FilterUpcomingStreams(copied), nil
	}

	// Holodex API는 콤마 구분 복수 status를 지원
	// 기존 2회 호출을 단일 호출로 통합하여 latency 및 rate limit 부담 감소
	var statusStr string
	if includeLive {
		statusStr = string(domain.StreamStatusLive) + "," + string(domain.StreamStatusUpcoming)
	} else {
		statusStr = string(domain.StreamStatusUpcoming)
	}

	params := url.Values{}
	params.Set("channel_id", channelID)
	params.Set("status", statusStr)
	params.Set("type", "stream")
	params.Set("max_upcoming_hours", fmt.Sprintf("%d", hours))

	body, err := h.requester.DoRequest(ctx, "GET", "/live", params)
	if err != nil {
		h.logger.Error("Failed to get channel schedule",
			slog.String("channel_id", channelID),
			slog.String("status", statusStr),
			slog.Any("error", err),
		)

		if h.shouldUseFallback(ctx, err) && h.scraper != nil {
			h.logger.Warn("Using scraper fallback for channel schedule",
				slog.String("channel_id", channelID),
				slog.Any("error", err))

			return h.scraper.FetchChannel(ctx, channelID)
		}

		return nil, fmt.Errorf("get channel schedule: %w", err)
	}

	var rawStreams []StreamRaw
	if err := json.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channel schedule: %w", err)
	}

	allStreams := h.mapper.MapStreamsResponse(rawStreams)

	hololiveOnly := h.filter.FilterHololiveStreams(allStreams)

	slices.SortFunc(hololiveOnly, func(a, b *domain.Stream) int {
		aTime := int64(0)
		if a.StartScheduled != nil {
			aTime = a.StartScheduled.Unix()
		}
		bTime := int64(0)
		if b.StartScheduled != nil {
			bTime = b.StartScheduled.Unix()
		}
		return cmp.Compare(aTime, bTime)
	})

	result := hololiveOnly
	if !includeLive {
		result = h.filter.FilterUpcomingStreams(hololiveOnly)
	}

	h.cacheManager.SetChannelSchedule(ctx, channelID, hours, includeLive, result, constants.CacheTTL.ChannelSchedule)

	return result, nil
}

// SearchChannels: 채널 이름 검색 쿼리를 통해 해당하는 Hololive 채널 목록을 조회합니다.
func (h *Service) SearchChannels(ctx context.Context, query string) ([]*domain.Channel, error) {
	if cached, found := h.cacheManager.GetSearchChannels(ctx, query); found {
		return cached, nil
	}

	query = stringutil.TrimSpace(query)
	params := url.Values{}
	params.Set("org", constants.HolodexAPIParams.OrgHololive)
	params.Set("type", constants.HolodexAPIParams.TypeVtuber)
	params.Set("limit", fmt.Sprintf("%d", constants.HolodexAPIParams.DefaultChannelLimit))

	body, err := h.requester.DoRequest(ctx, "GET", "/channels", params)
	if err != nil {
		h.logger.Error("Failed to search channels", slog.String("query", query), slog.Any("error", err))
		return nil, fmt.Errorf("search channels: %w", err)
	}

	var rawChannels []ChannelRaw
	if err := json.Unmarshal(body, &rawChannels); err != nil {
		return nil, fmt.Errorf("failed to unmarshal search channels: %w", err)
	}

	channels := h.mapper.MapChannelsResponse(rawChannels)

	h.logger.Debug("Holodex API search results",
		slog.String("query", query),
		slog.Int("total_results", len(channels)),
	)

	filtered := make([]*domain.Channel, 0, len(channels))
	normalizedQuery := strings.ToLower(query)
	for _, ch := range channels {
		if ch.Org != nil && *ch.Org == "Hololive" && !h.filter.IsHolostarsChannel(ch) {
			// 쿼리가 비어있으면 모든 채널 반환, 아니면 이름 매칭 필터링
			if normalizedQuery == "" {
				filtered = append(filtered, ch)
				continue
			}
			// 채널 이름 또는 영어 이름에 쿼리가 포함되는지 확인
			nameMatch := strings.Contains(strings.ToLower(ch.Name), normalizedQuery)
			englishMatch := ch.EnglishName != nil && strings.Contains(strings.ToLower(*ch.EnglishName), normalizedQuery)
			if nameMatch || englishMatch {
				filtered = append(filtered, ch)
			}
		}
	}

	h.logger.Debug("After HOLOSTARS filter", slog.Int("count", len(filtered)))

	h.cacheManager.SetSearchChannels(ctx, query, filtered)

	return filtered, nil
}

func buildSearchChannelsCacheKey(query string) string {
	normalized := stringutil.Normalize(query)
	if normalized == "" {
		return searchChannelsCacheKeyPrefix + "empty"
	}

	sum := sha256.Sum256([]byte(normalized))
	return searchChannelsCacheKeyPrefix + hex.EncodeToString(sum[:])
}

// GetChannel: 채널 ID로 특정 채널의 상세 정보를 조회합니다.
// retryable Holodex 오류(5xx/timeout/circuit/key rotation)에서만 YouTube 스크래퍼로 폴백하고,
// non-retryable 오류는 그대로 반환합니다.
func (h *Service) GetChannel(ctx context.Context, channelID string) (*domain.Channel, error) {
	if cached, found := h.cacheManager.GetChannel(ctx, channelID); found {
		return cached, nil
	}

	channel, err := h.fetchChannelDirect(ctx, channelID)
	if err == nil {
		return channel, nil
	}

	if h.shouldUseFallback(ctx, err) {
		h.logger.Warn("Using scraper fallback for channel",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)

		channel, fallbackErr := h.getChannelFromScraper(ctx, channelID)
		if fallbackErr == nil {
			return channel, nil
		}

		return nil, fmt.Errorf(
			"get channel: primary and scraper fallback failed: %w",
			stdErrors.Join(err, fallbackErr),
		)
	}

	h.logger.Error("Failed to get channel", slog.String("channel_id", channelID), slog.Any("error", err))
	return nil, fmt.Errorf("get channel: %w", err)
}

func (h *Service) fetchChannelDirect(ctx context.Context, channelID string) (*domain.Channel, error) {
	body, err := h.requester.DoRequest(ctx, "GET", "/channels/"+channelID, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch channel direct: %w", err)
	}

	var rawChannel ChannelRaw
	if err := json.Unmarshal(body, &rawChannel); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channel: %w", err)
	}

	channel := h.mapper.MapChannelResponse(&rawChannel)
	h.cacheManager.SetChannel(ctx, channelID, channel)

	return channel, nil
}

// getChannelFromScraper: YouTube 스크래퍼를 사용하여 채널 정보를 조회합니다. (Holodex 폴백)
func (h *Service) getChannelFromScraper(ctx context.Context, channelID string) (*domain.Channel, error) {
	if h.scraper == nil {
		return nil, fmt.Errorf("scraper fallback not configured")
	}

	stats, err := h.scraper.GetChannelStats(ctx, channelID)
	if err != nil {
		h.logger.Warn("Scraper fallback also failed for channel",
			slog.String("channel", channelID),
			slog.Any("error", err))
		return nil, fmt.Errorf("get channel stats from scraper: %w", err)
	}

	// 스크래퍼 데이터로 채널 정보 구성
	subCount := int(stats.SubscriberCount)
	channel := &domain.Channel{
		ID:              channelID,
		SubscriberCount: &subCount,
	}

	// GetChannelSnippet으로 아바타 가져오기
	snippet, snippetErr := h.scraper.GetChannelSnippet(ctx, channelID)
	if snippetErr == nil && snippet != nil {
		if len(snippet.Avatar) > 0 {
			channel.Photo = &snippet.Avatar[len(snippet.Avatar)-1].URL
		}
		// Banner 필드는 domain.Channel에 없으므로 생략
	}

	h.cacheManager.SetChannel(ctx, channelID, channel)

	h.logger.Info("Channel fetched via scraper fallback",
		slog.String("channel", channelID),
		slog.Int64("subscribers", stats.SubscriberCount))

	return channel, nil
}

// GetChannels: 여러 채널 ID로 채널 정보를 배치 조회합니다.
// 캐시를 우선 조회하고, 캐시 미스된 채널은 /channels 리스트 API로 한 번에 조회합니다.
// 기존 N+1 개별 호출 패턴을 단일 호출로 최적화하여 rate limit 부담을 대폭 감소시킵니다.
func (h *Service) GetChannels(ctx context.Context, channelIDs []string) (map[string]*domain.Channel, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*domain.Channel), nil
	}

	result := make(map[string]*domain.Channel, len(channelIDs))
	var missedIDs []string

	// 캐시에서 먼저 조회
	for _, id := range channelIDs {
		if cached, found := h.cacheManager.GetChannel(ctx, id); found {
			result[id] = cached
		} else {
			missedIDs = append(missedIDs, id)
		}
	}

	h.logger.Debug("GetChannels cache status",
		slog.Int("total", len(channelIDs)),
		slog.Int("cache_hits", len(result)),
		slog.Int("cache_misses", len(missedIDs)),
	)

	// 캐시 미스가 없으면 바로 반환
	if len(missedIDs) == 0 {
		return result, nil
	}

	// 캐시 미스된 채널을 /channels 리스트 API로 한 번에 조회
	// Holodex /channels API는 ID 필터를 지원하지 않으므로 org로 전체 조회 후 필터링
	allChannels, err := h.fetchHololiveChannelList(ctx)
	if err != nil {
		if !h.shouldUseFallback(ctx, err) {
			return result, fmt.Errorf("get channels batch list: %w", err)
		}

		h.logger.Warn("Failed to fetch channel list, falling back to individual queries",
			slog.Any("error", err),
			slog.Int("missed_count", len(missedIDs)),
		)
		// 폴백: 개별 조회 (기존 방식, 최대 5개 동시)
		return h.fetchChannelsIndividually(ctx, channelIDs, result, missedIDs)
	}

	// 필요한 채널만 결과에 추가하고 캐시 저장
	missedSet := make(map[string]bool, len(missedIDs))
	for _, id := range missedIDs {
		missedSet[id] = true
	}

	for _, ch := range allChannels {
		if missedSet[ch.ID] {
			result[ch.ID] = ch
			// 개별 캐시 저장
			h.cacheManager.SetChannel(ctx, ch.ID, ch)
		}
	}

	h.logger.Info("GetChannels batch complete (optimized)",
		slog.Int("requested", len(channelIDs)),
		slog.Int("returned", len(result)),
		slog.Int("from_list_api", len(result)-len(channelIDs)+len(missedIDs)),
	)

	return result, nil
}

// GetChannelsLiveStatus: 특정 채널들의 현재 생방송/예정 상태를 빠르게 조회합니다.
// /users/live 엔드포인트를 우선 사용하고, retryable 오류에서만 채널별 YouTube scraper 경로로 제한 폴백합니다.
// 이 경로는 공식 스케줄 페이지 재조회 없이 YouTube scraper 결과만 사용합니다.
// 주의: org, status, sort 필터링 미지원 - live+upcoming 모두 반환됨
// 사용 시나리오: 알림 체크, 대시보드 상태 표시 등 빠른 상태 확인
func (h *Service) GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	if len(channelIDs) == 0 {
		return []*domain.Stream{}, nil
	}

	if cached, found := h.cacheManager.GetChannelsLiveStatusStreams(ctx, channelIDs); found {
		return cached, nil
	}

	params := url.Values{}
	params.Set("channels", strings.Join(channelIDs, ","))

	body, err := h.requester.DoRequest(ctx, "GET", "/users/live", params)
	if err != nil {
		h.logger.Error("Failed to get channels live status",
			slog.Int("channel_count", len(channelIDs)),
			slog.Any("error", err),
		)

		// 스크래퍼 폴백 시도 (각 채널별로 YouTube 스크래핑)
		if h.shouldUseFallback(ctx, err) && h.scraper != nil {
			h.logger.Warn("Using scraper fallback for channels live status", slog.Any("error", err))
			allStreams, fallbackErr := h.getChannelsLiveStatusFromScraper(ctx, channelIDs)
			if fallbackErr != nil {
				h.logger.Warn("Scraper live status fallback failed",
					slog.Int("channel_count", len(channelIDs)),
					slog.Any("error", fallbackErr),
				)
			}
			if len(allStreams) > 0 {
				h.cacheManager.SetChannelsLiveStatusStreams(ctx, channelIDs, allStreams, 30*time.Second)
				return allStreams, nil
			}
		}

		return nil, fmt.Errorf("get channels live status: %w", err)
	}

	var rawStreams []StreamRaw
	if err := json.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("failed to unmarshal channels live status: %w", err)
	}

	streams := h.mapper.MapStreamsResponse(rawStreams)
	filtered := h.filter.FilterHololiveStreams(streams)

	// /users/live는 캐시된 결과이므로 짧은 TTL 적용 (30초)
	h.cacheManager.SetChannelsLiveStatusStreams(ctx, channelIDs, filtered, 30*time.Second)

	h.logger.Debug("GetChannelsLiveStatus completed",
		slog.Int("requested_channels", len(channelIDs)),
		slog.Int("streams_found", len(filtered)),
	)

	return filtered, nil
}

func (h *Service) getChannelsLiveStatusFromScraper(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	allStreams := make([]*domain.Stream, 0, len(channelIDs))
	var lastErr error

	for _, channelID := range channelIDs {
		streams, err := h.scraper.fetchFromYouTubeScraper(ctx, channelID)
		if err != nil {
			lastErr = err
			continue
		}
		allStreams = append(allStreams, streams...)
	}

	if lastErr != nil {
		return allStreams, fmt.Errorf("fetch channels live status from scraper: %w", lastErr)
	}

	return allStreams, nil
}

// fetchHololiveChannelList: Hololive 채널 목록을 /channels API로 조회합니다.
// 내부 캐시를 사용하여 반복 호출 시 효율을 높입니다.
// Holodex API limit=50 제한으로 인해 페이지네이션을 사용합니다.
func (h *Service) fetchHololiveChannelList(ctx context.Context) ([]*domain.Channel, error) {
	if cached, found := h.cacheManager.GetHololiveChannelList(ctx); found {
		return cached, nil
	}

	var allChannels []*domain.Channel
	pageSize := constants.HolodexAPIParams.DefaultChannelLimit
	offset := 0

	for {
		params := url.Values{}
		params.Set("org", constants.HolodexAPIParams.OrgHololive)
		params.Set("type", constants.HolodexAPIParams.TypeVtuber)
		params.Set("limit", fmt.Sprintf("%d", pageSize))
		params.Set("offset", fmt.Sprintf("%d", offset))

		body, err := h.requester.DoRequest(ctx, "GET", "/channels", params)
		if err != nil {
			return nil, fmt.Errorf("fetch hololive channel list (offset=%d): %w", offset, err)
		}

		var rawChannels []ChannelRaw
		if err := json.Unmarshal(body, &rawChannels); err != nil {
			return nil, fmt.Errorf("failed to unmarshal channel list: %w", err)
		}

		channels := h.mapper.MapChannelsResponse(rawChannels)
		allChannels = append(allChannels, channels...)

		// 마지막 페이지면 종료
		if len(rawChannels) < pageSize {
			break
		}

		offset += pageSize

		// 무한 루프 방지 (최대 MaxPaginationOffset개)
		if offset >= constants.HolodexAPIParams.MaxPaginationOffset {
			h.logger.Warn("Pagination limit reached", slog.Int("offset", offset))
			break
		}
	}

	h.logger.Debug("Fetched all Hololive channels", slog.Int("total", len(allChannels)))

	// 5분간 캐시 (채널 정보는 자주 변하지 않음)
	h.cacheManager.SetHololiveChannelList(ctx, allChannels, 5*time.Minute)

	return allChannels, nil
}

// fetchChannelsIndividually: 개별 /channels/{id} API로 채널을 조회합니다. (폴백용)
func (h *Service) fetchChannelsIndividually(ctx context.Context, channelIDs []string, result map[string]*domain.Channel, missedIDs []string) (map[string]*domain.Channel, error) {
	const maxConcurrent = 5
	if len(missedIDs) == 0 {
		return result, nil
	}

	workerCount := min(maxConcurrent, len(missedIDs))

	jobs := make(chan string)
	resultChan := make(chan struct {
		id      string
		channel *domain.Channel
	}, len(missedIDs))

	var workerWG sync.WaitGroup
	worker := func() {
		defer workerWG.Done()
		for channelID := range jobs {
			select {
			case <-ctx.Done():
				resultChan <- struct {
					id      string
					channel *domain.Channel
				}{channelID, nil}
				continue
			default:
			}

			channel, err := h.fetchChannelDirect(ctx, channelID)
			if err != nil {
				h.logger.Warn("Failed to get channel in batch",
					slog.String("channel_id", channelID),
					slog.Any("error", err),
				)
				resultChan <- struct {
					id      string
					channel *domain.Channel
				}{channelID, nil}
				continue
			}

			resultChan <- struct {
				id      string
				channel *domain.Channel
			}{channelID, channel}
		}
	}

	workerWG.Add(workerCount)
	for range workerCount {
		go worker()
	}

	go func() {
		defer close(jobs)
		for _, id := range missedIDs {
			select {
			case <-ctx.Done():
				return
			case jobs <- id:
			}
		}
	}()

	go func() {
		workerWG.Wait()
		close(resultChan)
	}()

	for {
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("batch channel fetch canceled: %w", ctx.Err())
		case r, ok := <-resultChan:
			if !ok {
				h.logger.Info("GetChannels batch complete (fallback)",
					slog.Int("requested", len(channelIDs)),
					slog.Int("returned", len(result)),
				)
				return result, nil
			}
			if r.channel != nil {
				result[r.id] = r.channel
			}
		}
	}
}

func (h *Service) shouldUseFallback(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}

	// 호출자 context 만료 시 폴백하지 않음 (불필요한 스크래퍼 호출 방지)
	if ctx.Err() != nil {
		return false
	}

	if h.requester != nil && h.requester.IsCircuitOpen() {
		return true
	}

	apiErr := &APIError{}
	if stdErrors.As(err, &apiErr) && apiErr.StatusCode >= 500 {
		return true
	}

	keyRotationError := &KeyRotationError{}
	if stdErrors.As(err, &keyRotationError) {
		return true
	}

	// timeout 에러도 폴백 대상
	return isTimeoutError(err)
}

// fetchIndieStreams: 개인세 VTuber 채널의 라이브 스트림을 조회합니다.
// Holodex /users/live API를 사용하여 채널 ID 기반으로 조회합니다.
func (h *Service) fetchIndieStreams(ctx context.Context) ([]*domain.Stream, error) {
	if len(constants.IndieChannelIDs) == 0 {
		return nil, nil
	}

	params := url.Values{}
	params.Set("channels", strings.Join(constants.IndieChannelIDs, ","))

	body, err := h.requester.DoRequest(ctx, "GET", "/users/live", params)
	if err != nil {
		return nil, fmt.Errorf("fetch indie streams: %w", err)
	}

	var rawStreams []StreamRaw
	if err := json.Unmarshal(body, &rawStreams); err != nil {
		return nil, fmt.Errorf("unmarshal indie streams: %w", err)
	}

	streams := h.mapper.MapStreamsResponse(rawStreams)

	// Indie 스트림 org 태깅
	for _, s := range streams {
		if s.Channel != nil && (s.Channel.Org == nil || *s.Channel.Org == "") {
			indie := "Indie"
			s.Channel.Org = &indie
		}
	}

	return streams, nil
}
