package youtube

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/retry"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// Service: YouTube API와 상호작용하여 채널 및 영상 정보를 제공하는 서비스
type Service struct {
	service       *youtube.Service
	scraper       *scraper.Client // HTML 스크래퍼 (quota 절약용)
	cache         *cache.Service
	logger        *slog.Logger
	quotaUsed     int
	quotaMu       sync.Mutex
	quotaReset    time.Time
	channelToName map[string]string // channelID -> memberName (ChannelTitle 조회용)
	channelMu     sync.RWMutex
}

// NewService: YouTube 서비스 인스턴스를 생성합니다.
func NewYouTubeService(
	ctx context.Context,
	apiKey string,
	cacheSvc *cache.Service,
	scraperProxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) (*Service, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("YouTube API key is required")
	}

	service, err := youtube.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube service: %w", err)
	}

	ys := &Service{
		service:       service,
		scraper:       scraper.NewClient(scraper.WithProxy(scraperProxyConfig), scraper.WithRateLimiter(sharedRL)),
		cache:         cacheSvc,
		logger:        logger,
		quotaUsed:     0,
		quotaReset:    getNextQuotaReset(),
		channelToName: make(map[string]string),
	}

	// 캐시에서 채널 ID -> 멤버 이름 맵 초기화
	if cacheSvc != nil {
		ys.loadChannelNameMap(ctx)
	}

	logger.Info("YouTube backup service initialized",
		slog.Time("quotaReset", ys.quotaReset))

	return ys, nil
}

// SetScraperProxyEnabled: YouTube 서비스 내부 HTML 스크래퍼의 프록시 사용 여부를 런타임에 토글합니다.
func (ys *Service) SetScraperProxyEnabled(enabled bool) bool {
	if ys == nil || ys.scraper == nil {
		return false
	}
	return ys.scraper.SetProxyEnabled(enabled)
}

// ScraperProxyEnabled: YouTube 서비스 내부 HTML 스크래퍼의 프록시 활성 상태를 반환합니다.
func (ys *Service) ScraperProxyEnabled() bool {
	if ys == nil || ys.scraper == nil {
		return false
	}
	return ys.scraper.ProxyEnabled()
}

// getNextQuotaReset: YouTube API quota 리셋 시간 계산
// YouTube quota는 PST 자정에 리셋됨 = KST 17:00 (UTC+9)
func getNextQuotaReset() time.Time {
	// KST = UTC+9 고정 오프셋 사용 (시간대 파일 불필요)
	kst := time.FixedZone("KST", 9*60*60)
	now := time.Now().In(kst)

	// KST 17:00 = PST 자정
	resetHour := 17
	next := time.Date(now.Year(), now.Month(), now.Day(), resetHour, 0, 0, 0, kst)

	// 이미 오늘 17시가 지났으면 내일 17시
	if now.After(next) {
		next = next.AddDate(0, 0, 1)
	}

	return next
}

// loadChannelNameMap: 캐시에서 멤버 정보를 읽어 channelID -> memberName 맵을 구성
func (ys *Service) loadChannelNameMap(ctx context.Context) {
	if ys.cache == nil {
		return
	}

	memberMap, err := ys.cache.GetAllMembers(ctx)
	if err != nil {
		ys.logger.Warn("Failed to load member map for channel names", slog.Any("error", err))
		return
	}

	ys.channelMu.Lock()
	defer ys.channelMu.Unlock()

	// memberMap은 name -> channelID이므로 역변환
	for key, channelID := range memberMap {
		if channelID != "" {
			// key format: "name:org" → name 부분만 추출
			name := key
			if idx := strings.LastIndex(key, ":"); idx > 0 {
				name = key[:idx]
			}
			ys.channelToName[channelID] = name
		}
	}

	ys.logger.Debug("Channel name map loaded", slog.Int("count", len(ys.channelToName)))
}

// getChannelName: channelID로 멤버 이름 조회 (없으면 빈 문자열)
func (ys *Service) getChannelName(channelID string) string {
	ys.channelMu.RLock()
	defer ys.channelMu.RUnlock()
	return ys.channelToName[channelID]
}

func (ys *Service) checkQuota(cost int) error {
	ys.quotaMu.Lock()
	defer ys.quotaMu.Unlock()

	now := time.Now()
	if now.After(ys.quotaReset) {
		ys.quotaUsed = 0
		ys.quotaReset = getNextQuotaReset()
		ys.logger.Info("YouTube API quota auto-reset",
			slog.Time("nextReset", ys.quotaReset))
	}

	if ys.quotaUsed+cost > (constants.YouTubeConfig.DailyQuotaLimit - constants.YouTubeConfig.QuotaSafetyMargin) {
		return &QuotaExceededError{
			Used:      ys.quotaUsed,
			Limit:     constants.YouTubeConfig.DailyQuotaLimit,
			Requested: cost,
			ResetTime: ys.quotaReset,
		}
	}

	return nil
}

func (ys *Service) consumeQuota(cost int) {
	ys.quotaMu.Lock()
	defer ys.quotaMu.Unlock()

	ys.quotaUsed += cost
	remaining := constants.YouTubeConfig.DailyQuotaLimit - ys.quotaUsed

	ys.logger.Debug("YouTube API quota consumed",
		slog.Int("cost", cost),
		slog.Int("used", ys.quotaUsed),
		slog.Int("remaining", remaining),
		slog.Float64("usagePercent", float64(ys.quotaUsed)/float64(constants.YouTubeConfig.DailyQuotaLimit)*100))

	if remaining < constants.YouTubeConfig.QuotaSafetyMargin {
		ys.logger.Warn("YouTube API quota running low",
			slog.Int("remaining", remaining),
			slog.Time("resetTime", ys.quotaReset))
	}
}

// GetUpcomingStreams: 지정된 채널들의 예정된 방송(라이브 예정) 목록을 조회합니다.
// 스크래퍼를 우선 사용하고, 실패한 채널만 YouTube API로 폴백합니다.
func (ys *Service) GetUpcomingStreams(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	if len(channelIDs) > constants.YouTubeConfig.MaxChannelsPerCall {
		ys.logger.Warn("Too many channels requested, limiting to max",
			slog.Int("requested", len(channelIDs)),
			slog.Int("limited", constants.YouTubeConfig.MaxChannelsPerCall))
		channelIDs = channelIDs[:constants.YouTubeConfig.MaxChannelsPerCall]
	}

	sortedIDs := make([]string, len(channelIDs))
	copy(sortedIDs, channelIDs)
	slices.Sort(sortedIDs)
	cacheKey := fmt.Sprintf("youtube:upcoming:%s", strings.Join(sortedIDs, ","))
	if cached, found := ys.cache.GetStreams(ctx, cacheKey); found {
		ys.logger.Debug("YouTube cache hit (backup avoided)",
			slog.Int("streams", len(cached)))
		return cached, nil
	}

	var allStreams []*domain.Stream
	var mu sync.Mutex
	var failedIDs []string
	var failedMu sync.Mutex

	// Phase 1: 스크래퍼로 우선 시도 (quota 0)
	g, gctx := errgroup.WithContext(ctx)
	semaphore := make(chan struct{}, 5) // 동시 요청 제한

	for _, channelID := range channelIDs {
		g.Go(func() error {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			events, err := ys.scraper.GetUpcomingEvents(gctx, channelID)
			if err != nil {
				// 스크래핑 실패 -> API 폴백 대상
				failedMu.Lock()
				failedIDs = append(failedIDs, channelID)
				failedMu.Unlock()
				return nil
			}
			if len(events) == 0 {
				// 예정/라이브 방송이 없는 정상 케이스는 API 폴백하지 않음.
				return nil
			}

			// 스크래핑 결과를 domain.Stream으로 변환
			streams := ys.convertScrapedEvents(events, channelID)
			mu.Lock()
			allStreams = append(allStreams, streams...)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	scraped := len(channelIDs) - len(failedIDs)
	ys.logger.Info("Scraper phase completed (upcoming streams)",
		slog.Int("total", len(channelIDs)),
		slog.Int("scraped", scraped),
		slog.Int("failed", len(failedIDs)))

	// Phase 2: 스크래핑 실패 채널만 YouTube API로 폴백
	if len(failedIDs) > 0 {
		estimatedCost := len(failedIDs) * constants.YouTubeConfig.SearchQuotaCost
		if err := ys.checkQuota(estimatedCost); err != nil {
			ys.logger.Warn("Quota exceeded for API fallback, returning partial results",
				slog.Int("scraped_count", len(allStreams)),
				slog.Any("error", err))
			// 부분 결과라도 캐시하고 반환
			if len(allStreams) > 0 {
				ys.cache.SetStreams(ctx, cacheKey, allStreams, constants.YouTubeConfig.CacheExpiration)
			}
			return allStreams, nil
		}

		ys.logger.Info("Fetching from YouTube API (fallback for failed scrapers)",
			slog.Int("channels", len(failedIDs)),
			slog.Int("estimatedCost", estimatedCost))

		apiStreams, apiCost := ys.fetchUpcomingFromAPI(ctx, failedIDs)
		mu.Lock()
		allStreams = append(allStreams, apiStreams...)
		mu.Unlock()

		ys.consumeQuota(apiCost)
	}

	ys.cache.SetStreams(ctx, cacheKey, allStreams, constants.YouTubeConfig.CacheExpiration)

	ys.logger.Info("Upcoming streams fetch completed (scraper+API)",
		slog.Int("channels", len(channelIDs)),
		slog.Int("streams", len(allStreams)),
		slog.Int("scraped", scraped),
		slog.Int("api_fallback", len(failedIDs)))

	return allStreams, nil
}

// convertScrapedEvents: 스크래핑된 UpcomingEvent를 domain.Stream으로 변환
func (ys *Service) convertScrapedEvents(events []*scraper.UpcomingEvent, channelID string) []*domain.Stream {
	streams := make([]*domain.Stream, 0, len(events))

	channelName := ys.getChannelName(channelID)
	if channelName == "" {
		channelName = events[0].ChannelTitle
	}

	for _, event := range events {
		// LIVE 또는 UPCOMING 상태만 포함
		if event.Status != "LIVE" && event.Status != "UPCOMING" {
			continue
		}

		stream := &domain.Stream{
			ID:          event.VideoID,
			Title:       event.Title,
			ChannelID:   channelID,
			ChannelName: channelName,
			Status:      ys.mapEventStatus(event.Status),
		}

		// 썸네일 설정
		if len(event.Thumbnail) > 0 {
			thumbURL := event.Thumbnail[len(event.Thumbnail)-1].URL
			stream.Thumbnail = &thumbURL
		} else {
			thumbURL := fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", event.VideoID)
			stream.Thumbnail = &thumbURL
		}

		// 링크 설정
		linkURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", event.VideoID)
		stream.Link = &linkURL

		// 시작 시간 설정
		if event.StartTime != nil {
			startTime := time.Unix(*event.StartTime, 0)
			stream.StartScheduled = &startTime
		}

		streams = append(streams, stream)
	}

	return streams
}

// mapEventStatus: 스크래퍼 상태를 domain.StreamStatus로 변환
func (ys *Service) mapEventStatus(status string) domain.StreamStatus {
	switch status {
	case "LIVE":
		return domain.StreamStatusLive
	case "UPCOMING":
		return domain.StreamStatusUpcoming
	default:
		return domain.StreamStatusUpcoming
	}
}

// fetchUpcomingFromAPI: YouTube API로 예정 방송 조회 (내부 폴백용)
func (ys *Service) fetchUpcomingFromAPI(ctx context.Context, channelIDs []string) ([]*domain.Stream, int) {
	results := make([]*domain.Stream, 0, len(channelIDs))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(constants.YouTubeConfig.MaxConcurrentRequests)

	actualCost := 0
	costMu := sync.Mutex{}

	for _, channelID := range channelIDs {
		g.Go(func() error {
			streams, err := ys.getChannelUpcomingStreams(gctx, channelID)
			if err != nil {
				ys.logger.Warn("Failed to fetch channel from API",
					slog.String("channelID", channelID),
					slog.Any("error", err))
				return nil
			}

			mu.Lock()
			results = append(results, streams...)
			mu.Unlock()

			costMu.Lock()
			actualCost += constants.YouTubeConfig.SearchQuotaCost
			costMu.Unlock()

			return nil
		})
	}

	_ = g.Wait()

	return results, actualCost
}

// processUpcomingStreams: YouTube API 응답에서 예정된 방송 정보를 추출하고 가공한다.
func (ys *Service) getChannelUpcomingStreams(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	call := ys.service.Search.List([]string{"snippet"}).
		ChannelId(channelID).
		Type("video").
		EventType("upcoming").
		MaxResults(int64(constants.YouTubeConfig.SearchMaxResults)).
		Order("date")

	var response *youtube.SearchListResponse
	err := ys.withRetry(ctx, func(c context.Context) error {
		var reqErr error
		response, reqErr = call.Context(c).Do()
		if reqErr != nil {
			apiErr := &googleapi.Error{}
			if errors.As(reqErr, &apiErr) && apiErr.Code == 403 {
				return &QuotaExceededError{
					Used:      ys.quotaUsed,
					Limit:     constants.YouTubeConfig.DailyQuotaLimit,
					Requested: constants.YouTubeConfig.SearchQuotaCost,
					ResetTime: ys.quotaReset,
				}
			}
			return fmt.Errorf("search request failed: %w", reqErr)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("YouTube API error: %w", err)
	}

	streams := make([]*domain.Stream, 0, len(response.Items))
	for _, item := range response.Items {
		if item.Id == nil || item.Id.VideoId == "" {
			continue
		}

		stream := &domain.Stream{
			ID:        item.Id.VideoId,
			Title:     item.Snippet.Title,
			ChannelID: channelID,
			Status:    domain.StreamStatusUpcoming,
			Link:      new(fmt.Sprintf("https://www.youtube.com/watch?v=%s", item.Id.VideoId)),
			Thumbnail: extractThumbnail(item.Snippet.Thumbnails),
		}

		if item.Snippet.PublishedAt != "" {
			if startTime, err := time.Parse(time.RFC3339, item.Snippet.PublishedAt); err == nil {
				stream.StartScheduled = &startTime
			}
		}

		if item.Snippet.ChannelTitle != "" {
			stream.Channel = &domain.Channel{
				ID:   channelID,
				Name: item.Snippet.ChannelTitle,
			}
		}

		streams = append(streams, stream)
	}

	return streams, nil
}

func extractThumbnail(thumbnails *youtube.ThumbnailDetails) *string {
	if thumbnails == nil {
		return nil
	}

	if thumbnails.Maxres != nil && thumbnails.Maxres.Url != "" {
		return &thumbnails.Maxres.Url
	}
	if thumbnails.High != nil && thumbnails.High.Url != "" {
		return &thumbnails.High.Url
	}
	if thumbnails.Medium != nil && thumbnails.Medium.Url != "" {
		return &thumbnails.Medium.Url
	}
	if thumbnails.Default != nil && thumbnails.Default.Url != "" {
		return &thumbnails.Default.Url
	}

	return nil
}

// GetQuotaStatus: 현재 API 할당량 사용량, 잔여량, 초기화 예정 시간을 반환합니다.
func (ys *Service) GetQuotaStatus() (used int, remaining int, resetTime time.Time) {
	ys.quotaMu.Lock()
	defer ys.quotaMu.Unlock()

	if time.Now().After(ys.quotaReset) {
		return 0, constants.YouTubeConfig.DailyQuotaLimit, getNextQuotaReset()
	}

	return ys.quotaUsed, constants.YouTubeConfig.DailyQuotaLimit - ys.quotaUsed, ys.quotaReset
}

// IsQuotaAvailable: 지정된 채널 수만큼 조회할 수 있는 API 할당량이 남아있는지 확인합니다.
func (ys *Service) IsQuotaAvailable(channelCount int) bool {
	estimatedCost := channelCount * constants.YouTubeConfig.SearchQuotaCost
	err := ys.checkQuota(estimatedCost)
	return err == nil
}

// QuotaExceededError: API 할당량 초과 시 발생하는 에러 구조체
type QuotaExceededError struct {
	Used      int
	Limit     int
	Requested int
	ResetTime time.Time
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("YouTube API quota exceeded: used %d/%d (requested %d more), resets at %s",
		e.Used, e.Limit, e.Requested, e.ResetTime.Format(time.RFC3339))
}

// GetChannelStatistics: 지정된 채널들의 통계(구독자 수, 조회수 등)를 조회합니다.
// 스크래퍼를 우선 사용하고, 실패한 채널만 YouTube API로 폴백합니다.
// 이 방식으로 YouTube API quota를 절약합니다.
func (ys *Service) GetChannelStatistics(ctx context.Context, channelIDs []string) (map[string]*ChannelStats, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*ChannelStats), nil
	}

	result := make(map[string]*ChannelStats)
	var mu sync.Mutex
	var failedIDs []string
	var failedMu sync.Mutex

	// Phase 1: 병렬 스크래핑으로 우선 시도
	// HTTP context 취소와 독립적으로 실행 (WithoutCancel + 자체 타임아웃)
	scraperCtx, scraperCancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		constants.YouTubeConfig.ScraperPhaseTimeout,
	)
	defer scraperCancel()

	g, gctx := errgroup.WithContext(scraperCtx)
	semaphore := make(chan struct{}, 5) // 동시 요청 제한 (rate limiting 방지)

	for _, channelID := range channelIDs {
		g.Go(func() error {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			stats, err := ys.scraper.GetChannelStats(gctx, channelID)
			if err != nil {
				ys.logger.Debug("Scraper failed, will fallback to API",
					slog.String("channelID", channelID),
					slog.Any("error", err))
				failedMu.Lock()
				failedIDs = append(failedIDs, channelID)
				failedMu.Unlock()
				return nil
			}

			mu.Lock()
			// 캐시에서 채널 이름 조회, 없으면 Handle 사용
			channelTitle := ys.getChannelName(channelID)
			if channelTitle == "" {
				channelTitle = stats.Handle
			}
			result[channelID] = &ChannelStats{
				ChannelID:       stats.ChannelID,
				ChannelTitle:    channelTitle,
				SubscriberCount: uint64(stats.SubscriberCount),
				VideoCount:      uint64(stats.VideoCount),
				ViewCount:       uint64(stats.ViewCount),
				Timestamp:       time.Now(),
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	scraped := len(channelIDs) - len(failedIDs)
	ys.logger.Info("Scraper phase completed",
		slog.Int("total", len(channelIDs)),
		slog.Int("scraped", scraped),
		slog.Int("failed", len(failedIDs)))

	// Phase 2: 스크래핑 실패한 채널만 YouTube API로 폴백
	if len(failedIDs) > 0 {
		apiCtx, apiCancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			constants.YouTubeConfig.APIFallbackTimeout,
		)
		defer apiCancel()

		apiResult, err := ys.getChannelStatsFromAPI(apiCtx, failedIDs)
		if err != nil {
			ys.logger.Warn("API fallback failed",
				slog.Int("channels", len(failedIDs)),
				slog.Any("error", err))
			// 부분 결과라도 반환
		} else {
			mu.Lock()
			maps.Copy(result, apiResult)
			mu.Unlock()
		}
	}

	ys.logger.Info("Channel statistics fetched (scraper+API)",
		slog.Int("channels", len(channelIDs)),
		slog.Int("results", len(result)),
		slog.Int("scraped", scraped),
		slog.Int("api_fallback", len(failedIDs)))

	return result, nil
}

// getChannelStatsFromAPI: YouTube Data API를 사용하여 채널 통계 조회 (내부 폴백용)
func (ys *Service) getChannelStatsFromAPI(ctx context.Context, channelIDs []string) (map[string]*ChannelStats, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*ChannelStats), nil
	}

	cost := len(channelIDs) * constants.YouTubeConfig.ChannelsQuotaCost
	if err := ys.checkQuota(cost); err != nil {
		return nil, err
	}

	// 배치로 분할
	batchSize := 50
	var batches [][]string
	for i := 0; i < len(channelIDs); i += batchSize {
		end := min(i+batchSize, len(channelIDs))
		batches = append(batches, channelIDs[i:end])
	}

	result := make(map[string]*ChannelStats)
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)

	for _, batch := range batches {
		g.Go(func() error {
			call := ys.service.Channels.List([]string{"statistics", "snippet"}).
				Id(batch...)

			response, err := call.Context(gctx).Do()
			if err != nil {
				ys.logger.Error("Failed to fetch channel statistics from API",
					slog.Int("batch_size", len(batch)),
					slog.Any("error", err))
				return nil
			}

			now := time.Now()
			mu.Lock()
			for _, channel := range response.Items {
				result[channel.Id] = &ChannelStats{
					ChannelID:       channel.Id,
					ChannelTitle:    channel.Snippet.Title,
					SubscriberCount: channel.Statistics.SubscriberCount,
					VideoCount:      channel.Statistics.VideoCount,
					ViewCount:       channel.Statistics.ViewCount,
					Timestamp:       now,
				}
			}
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()
	ys.consumeQuota(cost)

	ys.logger.Info("API fallback completed",
		slog.Int("channels", len(channelIDs)),
		slog.Int("results", len(result)),
		slog.Int("quota_used", cost))

	return result, nil
}

// GetRecentVideos: 특정 채널의 최근 업로드된 비디오 목록을 조회합니다.
// 스크래퍼를 우선 사용하고, 실패 시 YouTube API로 폴백합니다.
func (ys *Service) GetRecentVideos(ctx context.Context, channelID string, maxResults int64) ([]string, error) {
	// Phase 1: 스크래퍼로 우선 시도 (quota 0)
	videos, err := ys.scraper.GetRecentVideos(ctx, channelID, int(maxResults))
	if err == nil && len(videos) > 0 {
		videoIDs := make([]string, 0, len(videos))
		for _, v := range videos {
			videoIDs = append(videoIDs, v.VideoID)
		}
		ys.logger.Debug("Recent videos fetched via scraper",
			slog.String("channel", channelID),
			slog.Int("count", len(videoIDs)))
		return videoIDs, nil
	}

	// Phase 2: 스크래핑 실패 시 API 폴백
	ys.logger.Debug("Scraper failed, falling back to API",
		slog.String("channel", channelID),
		slog.Any("scraper_error", err))

	if quotaErr := ys.checkQuota(constants.YouTubeConfig.SearchQuotaCost); quotaErr != nil {
		return nil, quotaErr
	}

	call := ys.service.Search.List([]string{"id"}).
		ChannelId(channelID).
		Type("video").
		Order("date").
		MaxResults(maxResults)

	response, err := call.Context(ctx).Do()
	if err != nil {
		ys.logger.Error("Failed to fetch recent videos",
			slog.String("channel", channelID),
			slog.Any("error", err))
		return nil, fmt.Errorf("YouTube search error: %w", err)
	}

	videoIDs := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		if item.Id != nil && item.Id.VideoId != "" {
			videoIDs = append(videoIDs, item.Id.VideoId)
		}
	}

	ys.consumeQuota(constants.YouTubeConfig.SearchQuotaCost)

	ys.logger.Debug("Recent videos fetched via API",
		slog.String("channel", channelID),
		slog.Int("count", len(videoIDs)))

	return videoIDs, nil
}

// ChannelStats: API로부터 조회된 단일 채널의 통계 정보
type ChannelStats struct {
	ChannelID       string
	ChannelTitle    string
	SubscriberCount uint64
	VideoCount      uint64
	ViewCount       uint64
	Timestamp       time.Time
}

func (ys *Service) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var quotaErr *QuotaExceededError
	if errors.As(err, &quotaErr) {
		return false
	}

	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case 429:
			return true
		case 500, 502, 503, 504:
			return true
		case 400, 401, 403, 404:
			return false
		}
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	return true
}

func (ys *Service) withRetry(ctx context.Context, fn func(context.Context) error) error {
	err := retry.WithRetry(ctx, retry.RetryOptions{
		MaxAttempts: constants.RetryConfig.MaxAttempts,
		BaseDelay:   constants.RetryConfig.BaseDelay,
		Jitter:      constants.RetryConfig.Jitter,
		ShouldRetry: ys.isRetryableError,
		OnRetry: func(attempt int, err error, delay time.Duration) {
			ys.logger.Warn("YouTube API retry",
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
				slog.Any("error", err),
			)
		},
	}, fn)
	if err != nil {
		return fmt.Errorf("youtube retry exhausted: %w", err)
	}
	return nil
}
