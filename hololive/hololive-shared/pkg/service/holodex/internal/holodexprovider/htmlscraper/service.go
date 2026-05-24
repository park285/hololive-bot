package htmlscraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/fallback"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type Service struct {
	httpClient      *http.Client
	cache           cache.StreamCache
	membersData     domain.MemberDataProvider
	memberNameMap   map[string]string
	logger          *slog.Logger
	baseURL         string
	youtubeProducer *scraper.Client
	fetchUpcoming   func(ctx context.Context, channelID string) ([]*scraper.UpcomingEvent, error)
	officialPageMu  sync.RWMutex
	officialPage    officialSchedulePageCache
	officialGroup   singleflight.Group
	nowFunc         func() time.Time
}

const (
	ChannelCacheKeyPrefix        = "scraper:channel:"
	OfficialSchedulePageCacheKey = "official_schedule_page"
)

type officialSchedulePageCache struct {
	streams   []*domain.Stream
	expiresAt time.Time
}

func NewService(
	cacheClient cache.StreamCache,
	membersData domain.MemberDataProvider,
	youtubeProxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) *Service {
	return NewServiceWithYouTubeProducer(
		cacheClient,
		membersData,
		scraper.NewClient(scraper.WithProxy(youtubeProxyConfig), scraper.WithRateLimiter(sharedRL)),
		logger,
	)
}

func NewServiceWithYouTubeProducer(
	cacheClient cache.StreamCache,
	membersData domain.MemberDataProvider,
	youtubeProducer *scraper.Client,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	members := []*domain.Member(nil)
	if membersData != nil {
		members = membersData.GetAllMembers()
	}
	nameMap := buildMemberNameMap(membersData)
	logger.Info("Scraper initialized with member matching and YouTube fallback",
		slog.Int("members", len(members)),
		slog.Int("name_mappings", len(nameMap)))

	return &Service{
		httpClient:      httputil.NewExternalAPIClient(constants.OfficialScheduleConfig.Timeout),
		cache:           cacheClient,
		membersData:     membersData,
		memberNameMap:   nameMap,
		logger:          logger,
		baseURL:         constants.OfficialScheduleConfig.BaseURL,
		youtubeProducer: youtubeProducer,
	}
}

func (s *Service) FetchChannel(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	cacheKey := ChannelCacheKeyPrefix + channelID
	if cached, found := s.cache.GetStreams(ctx, cacheKey); found {
		s.logger.Debug("Scraper cache hit", slog.String("channel", channelID))
		return cached, nil
	}

	if s.youtubeProducer != nil || s.fetchUpcoming != nil {
		policy := fallback.Policy{Trigger: fallback.TriggerOnFailures}
		streams, err := s.FetchFromYouTubeProducer(ctx, channelID)
		fallback.ObservePrimaryPhase("holodex", "channel_schedule", 1, boolToInt(len(streams) > 0), boolToInt(err != nil))
		if !policy.ShouldRun(len(streams), boolToInt(err != nil)) {
			s.finishYouTubeChannelSchedule(ctx, cacheKey, channelID, streams, policy)
			return streams, nil
		}
		if err != nil {
			s.logger.Debug("YouTube producer failed, falling back to official schedule",
				slog.String("channel", channelID),
				slog.Any("error", err))
		}
	}

	return s.fetchOfficialChannelSchedule(ctx, cacheKey, channelID)
}

func (s *Service) finishYouTubeChannelSchedule(ctx context.Context, cacheKey string, channelID string, streams []*domain.Stream, policy fallback.Policy) {
	fallback.ObserveExecution("holodex", "channel_schedule", policy.Trigger, "skipped")
	s.cache.SetStreams(ctx, cacheKey, streams, constants.OfficialScheduleConfig.CacheExpiry)
	s.logger.Info("YouTube producer channel schedule resolved",
		slog.String("channel", channelID),
		slog.Int("streams", len(streams)))
}

func (s *Service) fetchOfficialChannelSchedule(ctx context.Context, cacheKey string, channelID string) ([]*domain.Stream, error) {
	s.logger.Info("Fetching from official schedule (FALLBACK MODE)",
		slog.String("channel", channelID),
		slog.String("url", s.baseURL))

	allStreams, err := s.fetchAllStreams(ctx)
	if err != nil {
		fallback.ObserveExecution("holodex", "channel_schedule", fallback.TriggerOnFailures, "error")
		reason := classifyOfficialScheduleFallbackReason(err, 0)
		observeOfficialScheduleFallback("channel_schedule", "error", reason)
		s.logger.Warn("Official schedule fallback failed",
			slog.String("channel", channelID),
			slog.String("reason", string(reason)),
			slog.Any("error", err))
		return nil, fmt.Errorf("all fallback sources failed: %w", err)
	}

	channelStreams := make([]*domain.Stream, 0)
	for _, stream := range allStreams {
		if stream.ChannelID == channelID {
			channelStreams = append(channelStreams, stream)
		}
	}

	s.cache.SetStreams(ctx, cacheKey, channelStreams, constants.OfficialScheduleConfig.CacheExpiry)
	outcome := "miss"
	reason := classifyOfficialScheduleFallbackReason(nil, len(channelStreams))
	if len(channelStreams) > 0 {
		outcome = "hit"
	}
	fallback.ObserveExecution("holodex", "channel_schedule", fallback.TriggerOnFailures, outcome)
	observeOfficialScheduleFallback("channel_schedule", outcome, reason)

	s.logger.Info("Official schedule scraper completed",
		slog.String("channel", channelID),
		slog.Int("streams", len(channelStreams)),
		slog.String("fallback_outcome", outcome),
		slog.String("fallback_reason", string(reason)))

	return channelStreams, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Service) FetchAllStreams(ctx context.Context) ([]*domain.Stream, error) {
	return s.fetchAllStreams(ctx)
}

func (s *Service) SetYouTubeProxyEnabled(enabled bool) bool {
	if s == nil || s.youtubeProducer == nil {
		return false
	}
	return s.youtubeProducer.SetProxyEnabled(enabled)
}

func (s *Service) YouTubeProxyEnabled() bool {
	if s == nil || s.youtubeProducer == nil {
		return false
	}
	return s.youtubeProducer.ProxyEnabled()
}

func (s *Service) ValidateStructure(ctx context.Context) error {
	_, err := s.fetchAllStreams(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch streams for structure validation: %w", err)
	}
	return nil
}

type StructureChangedError struct {
	Message     string
	ParseErrors int
}

func (e *StructureChangedError) Error() string {
	return fmt.Sprintf("%s (parse errors: %d)", e.Message, e.ParseErrors)
}

func IsStructureError(err error) bool {
	structureChangedError := &StructureChangedError{}
	return errors.As(err, &structureChangedError)
}

func (s *Service) GetRecentVideos(ctx context.Context, channelID string, maxResults int) ([]*scraper.Video, error) {
	if s.youtubeProducer == nil {
		return nil, fmt.Errorf("youtube producer not initialized")
	}
	videos, err := s.youtubeProducer.GetRecentVideos(ctx, channelID, maxResults)
	if err != nil {
		return nil, fmt.Errorf("youtube recent videos scraper error: %w", err)
	}
	s.logger.Debug("Recent videos fetched via scraper", slog.String("channel", channelID), slog.Int("count", len(videos)))
	return videos, nil
}

func (s *Service) GetChannelStats(ctx context.Context, channelID string) (*scraper.ChannelStats, error) {
	if s.youtubeProducer == nil {
		return nil, fmt.Errorf("youtube producer not initialized")
	}
	stats, err := s.youtubeProducer.GetChannelStats(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("youtube channel stats scraper error: %w", err)
	}
	s.logger.Debug("Channel stats fetched via scraper", slog.String("channel", channelID), slog.Int64("subscribers", stats.SubscriberCount))
	return stats, nil
}

func (s *Service) GetChannelSnippet(ctx context.Context, channelID string) (*scraper.ChannelSnippet, error) {
	if s.youtubeProducer == nil {
		return nil, fmt.Errorf("youtube producer not initialized")
	}
	snippet, err := s.youtubeProducer.GetChannelSnippet(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("youtube channel snippet scraper error: %w", err)
	}
	s.logger.Debug("Channel snippet fetched via scraper", slog.String("channel", channelID), slog.Int("avatars", len(snippet.Avatar)), slog.Int("banners", len(snippet.Banner)))
	return snippet, nil
}

func (s *Service) GetPopularVideos(ctx context.Context, channelID string, maxResults int) ([]*scraper.Video, error) {
	if s.youtubeProducer == nil {
		return nil, fmt.Errorf("youtube producer not initialized")
	}
	videos, err := s.youtubeProducer.GetPopularVideos(ctx, channelID, maxResults)
	if err != nil {
		return nil, fmt.Errorf("youtube popular videos scraper error: %w", err)
	}
	s.logger.Debug("Popular videos fetched via scraper", slog.String("channel", channelID), slog.Int("count", len(videos)))
	return videos, nil
}

func NewTestService(cacheClient cache.StreamCache, membersData domain.MemberDataProvider, logger *slog.Logger) *Service {
	return NewServiceWithYouTubeProducer(cacheClient, membersData, nil, logger)
}
