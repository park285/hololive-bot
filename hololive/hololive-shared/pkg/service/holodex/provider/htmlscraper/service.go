package htmlscraper

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/park285/shared-go/pkg/httputil"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/internal/service/fallback"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

type Service struct {
	httpClient           *http.Client
	cache                cache.StreamCache
	identityIndex        officialScheduleIdentityIndex
	logger               *slog.Logger
	officialSchedule     settings.OfficialScheduleConfig
	maxResponseBodyBytes int64
	youtubeProducer      *scraper.Client
	fetchUpcoming        func(ctx context.Context, channelID string) ([]*parser.UpcomingEvent, error)
	officialPageMu       sync.RWMutex
	officialPage         officialSchedulePageCache
	officialGroup        singleflight.Group
	nowFunc              func() time.Time
}

const (
	channelScheduleCacheKeyPrefix = "official_schedule:channel:"
	officialScheduleCacheKey      = "official_schedule_api:list:2"
)

type officialSchedulePageCache struct {
	streams   []*domain.Stream
	expiresAt time.Time
}

func NewService(
	cacheClient cache.StreamCache,
	membersData domain.MemberDataProvider,
	youtubeProxyConfig scraper.ProxyConfig,
	sharedRL *ratelimiter.RateLimiter,
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

	runtimeConfig := settings.LoadOfficialScheduleRuntimeConfig()
	identityIndex := buildOfficialScheduleIdentityIndex(membersData)
	logger.Info("Official schedule API source initialized",
		slog.String("path", officialScheduleAPIPath),
		slog.Int("identity_keys", len(identityIndex)))

	return &Service{
		httpClient:           httputil.NewExternalAPIClient(runtimeConfig.OfficialSchedule.Timeout),
		cache:                cacheClient,
		identityIndex:        identityIndex,
		logger:               logger,
		officialSchedule:     runtimeConfig.OfficialSchedule,
		maxResponseBodyBytes: runtimeConfig.MaxResponseBodyBytes,
		youtubeProducer:      youtubeProducer,
	}
}

func (s *Service) FetchChannel(ctx context.Context, channelID string, hours int, includeLive bool) ([]*domain.Stream, error) {
	cacheKey := channelScheduleCacheKey(channelID, hours, includeLive)
	if cached, found := s.getCachedChannelSchedule(ctx, cacheKey); found {
		return cached, nil
	}

	streams, sourceErr, resolved := s.fetchYouTubeChannelSchedule(ctx, channelID, hours, includeLive)
	if resolved {
		s.cacheChannelSchedule(ctx, cacheKey, streams)
		return streams, nil
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("fetch channel schedule: %w", ctx.Err())
	}

	return s.fetchOfficialChannelSchedule(ctx, cacheKey, channelID, hours, includeLive, sourceErr)
}

func (s *Service) fetchYouTubeChannelSchedule(
	ctx context.Context,
	channelID string,
	hours int,
	includeLive bool,
) ([]*domain.Stream, error, bool) {
	if s.youtubeProducer == nil && s.fetchUpcoming == nil {
		return nil, nil, false
	}

	streams, err := s.FetchFromYouTubeProducer(ctx, channelID)
	fallback.ObservePrimaryPhase("holodex", "channel_schedule", 1, boolToInt(len(streams) > 0), boolToInt(err != nil))
	if err != nil {
		s.logger.Debug("YouTube channel schedule failed; using official schedule API",
			slog.String("channel", channelID),
			slog.Any("error", err))
		return nil, err, false
	}

	fallback.ObserveExecution("holodex", "channel_schedule", fallback.TriggerOnFailures, "skipped")
	return filterScheduleWindow(streams, hours, includeLive, s.now()), nil, true
}

func (s *Service) fetchOfficialChannelSchedule(
	ctx context.Context,
	cacheKey string,
	channelID string,
	hours int,
	includeLive bool,
	primaryErr error,
) ([]*domain.Stream, error) {
	allStreams, err := s.fetchAllStreams(ctx)
	if err != nil {
		fallback.ObserveExecution("holodex", "channel_schedule", fallback.TriggerOnFailures, "error")
		observeOfficialScheduleFallback("channel_schedule", "error", classifyOfficialScheduleReason(err, 0))
		return nil, fmt.Errorf("channel schedule sources failed: %w", errors.Join(primaryErr, err))
	}

	channelStreams := filterChannelStreams(allStreams, channelID)
	channelStreams = filterScheduleWindow(channelStreams, hours, includeLive, s.now())
	s.cacheChannelSchedule(ctx, cacheKey, channelStreams)
	observeChannelScheduleOutcome(channelStreams)
	return channelStreams, nil
}

func observeChannelScheduleOutcome(streams []*domain.Stream) {
	outcome := "miss"
	if len(streams) > 0 {
		outcome = "hit"
	}
	fallback.ObserveExecution("holodex", "channel_schedule", fallback.TriggerOnFailures, outcome)
	observeOfficialScheduleFallback("channel_schedule", outcome, classifyOfficialScheduleReason(nil, len(streams)))
}

func filterChannelStreams(streams []*domain.Stream, channelID string) []*domain.Stream {
	filtered := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if stream != nil && stream.ChannelID == channelID {
			filtered = append(filtered, stream)
		}
	}
	return filtered
}

func filterScheduleWindow(streams []*domain.Stream, hours int, includeLive bool, now time.Time) []*domain.Stream {
	upperBound := time.Time{}
	if hours > 0 {
		upperBound = now.Add(time.Duration(hours) * time.Hour)
	}

	filtered := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if scheduleStreamAllowed(stream, includeLive, now, upperBound) {
			filtered = append(filtered, stream)
		}
	}
	slices.SortStableFunc(filtered, compareScheduledStreams)
	return filtered
}

func scheduleStreamAllowed(stream *domain.Stream, includeLive bool, now, upperBound time.Time) bool {
	if stream == nil {
		return false
	}
	if stream.Status == domain.StreamStatusLive {
		return includeLive
	}
	if stream.Status != domain.StreamStatusUpcoming || stream.StartActual != nil {
		return false
	}
	if stream.StartScheduled == nil {
		return true
	}
	if stream.StartScheduled.Before(now) {
		return false
	}
	return upperBound.IsZero() || !stream.StartScheduled.After(upperBound)
}

func compareScheduledStreams(left, right *domain.Stream) int {
	if left.StartScheduled == nil && right.StartScheduled == nil {
		return 0
	}
	if left.StartScheduled == nil {
		return 1
	}
	if right.StartScheduled == nil {
		return -1
	}
	return cmp.Compare(left.StartScheduled.UnixNano(), right.StartScheduled.UnixNano())
}

func channelScheduleCacheKey(channelID string, hours int, includeLive bool) string {
	return fmt.Sprintf("%s%s:%d:%t", channelScheduleCacheKeyPrefix, channelID, hours, includeLive)
}

func (s *Service) getCachedChannelSchedule(ctx context.Context, key string) ([]*domain.Stream, bool) {
	if s.cache == nil {
		return nil, false
	}
	cached, found := s.cache.GetStreams(ctx, key)
	if found {
		s.logger.Debug("Official schedule channel cache hit", slog.String("key", key))
	}
	return cached, found
}

func (s *Service) cacheChannelSchedule(ctx context.Context, key string, streams []*domain.Stream) {
	if s.cache == nil {
		return
	}
	s.cache.SetStreams(ctx, key, streams, s.officialSchedule.CacheExpiry)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Service) FetchUpcomingStreams(ctx context.Context, hours int) ([]*domain.Stream, error) {
	streams, err := s.fetchAllStreams(ctx)
	if err != nil {
		return nil, err
	}
	return filterScheduleWindow(streams, hours, false, s.now()), nil
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
		return fmt.Errorf("validate official schedule API: %w", err)
	}
	return nil
}

type StructureChangedError struct {
	Message     string
	InvalidRows int
}

func (e *StructureChangedError) Error() string {
	return fmt.Sprintf("%s (invalid rows: %d)", e.Message, e.InvalidRows)
}

func IsStructureError(err error) bool {
	structureChangedError := &StructureChangedError{}
	return errors.As(err, &structureChangedError)
}

func (s *Service) GetRecentVideos(ctx context.Context, channelID string, maxResults int) ([]*parser.Video, error) {
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

func (s *Service) GetChannelStats(ctx context.Context, channelID string) (*parser.ChannelStats, error) {
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

func (s *Service) GetChannelSnippet(ctx context.Context, channelID string) (*parser.ChannelSnippet, error) {
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

func (s *Service) GetPopularVideos(ctx context.Context, channelID string, maxResults int) ([]*parser.Video, error) {
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
