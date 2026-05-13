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

package holodex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/fallback"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type ScraperService struct {
	httpClient     *http.Client
	cache          cache.Client
	membersData    domain.MemberDataProvider
	memberNameMap  map[string]string
	logger         *slog.Logger
	baseURL        string
	youtubeScraper *scraper.Client
	fetchUpcoming  func(ctx context.Context, channelID string) ([]*scraper.UpcomingEvent, error)
	officialPageMu sync.RWMutex
	officialPage   officialSchedulePageCache
	officialGroup  singleflight.Group
	nowFunc        func() time.Time
}

const (
	scraperChannelCacheKeyPrefix = "scraper:channel:"
	officialSchedulePageCacheKey = "official_schedule_page"
)

type officialSchedulePageCache struct {
	streams   []*domain.Stream
	expiresAt time.Time
}

func NewScraperService(
	cacheSvc cache.Client,
	membersData domain.MemberDataProvider,
	youtubeProxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) *ScraperService {
	return NewScraperServiceWithYouTubeScraper(
		cacheSvc,
		membersData,
		scraper.NewClient(scraper.WithProxy(youtubeProxyConfig), scraper.WithRateLimiter(sharedRL)),
		logger,
	)
}

func NewScraperServiceWithYouTubeScraper(
	cacheSvc cache.Client,
	membersData domain.MemberDataProvider,
	youtubeScraper *scraper.Client,
	logger *slog.Logger,
) *ScraperService {
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

	return &ScraperService{
		httpClient:     httputil.NewExternalAPIClient(constants.OfficialScheduleConfig.Timeout),
		cache:          cacheSvc,
		membersData:    membersData,
		memberNameMap:  nameMap,
		logger:         logger,
		baseURL:        constants.OfficialScheduleConfig.BaseURL,
		youtubeScraper: youtubeScraper,
	}
}

func (s *ScraperService) FetchChannel(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	cacheKey := scraperChannelCacheKeyPrefix + channelID
	if cached, found := s.cache.GetStreams(ctx, cacheKey); found {
		s.logger.Debug("Scraper cache hit", slog.String("channel", channelID))
		return cached, nil
	}

	if s.youtubeScraper != nil || s.fetchUpcoming != nil {
		policy := fallback.Policy{Trigger: fallback.TriggerOnFailures}
		streams, err := s.fetchFromYouTubeScraper(ctx, channelID)
		fallback.ObservePrimaryPhase("holodex", "channel_schedule", 1, boolToInt(len(streams) > 0), boolToInt(err != nil))
		if !policy.ShouldRun(len(streams), boolToInt(err != nil)) {
			s.finishYouTubeChannelSchedule(ctx, cacheKey, channelID, streams, policy)
			return streams, nil
		}
		if err != nil {
			s.logger.Debug("YouTube scraper failed, falling back to official schedule",
				slog.String("channel", channelID),
				slog.Any("error", err))
		}
	}

	return s.fetchOfficialChannelSchedule(ctx, cacheKey, channelID)
}

func (s *ScraperService) finishYouTubeChannelSchedule(
	ctx context.Context,
	cacheKey string,
	channelID string,
	streams []*domain.Stream,
	policy fallback.Policy,
) {
	fallback.ObserveExecution("holodex", "channel_schedule", policy.Trigger, "skipped")
	s.cache.SetStreams(ctx, cacheKey, streams, constants.OfficialScheduleConfig.CacheExpiry)
	s.logger.Info("YouTube scraper channel schedule resolved",
		slog.String("channel", channelID),
		slog.Int("streams", len(streams)))
}

func (s *ScraperService) fetchOfficialChannelSchedule(
	ctx context.Context,
	cacheKey string,
	channelID string,
) ([]*domain.Stream, error) {
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

func (s *ScraperService) FetchAllStreams(ctx context.Context) ([]*domain.Stream, error) {
	return s.fetchAllStreams(ctx)
}

func (s *ScraperService) SetYouTubeProxyEnabled(enabled bool) bool {
	if s == nil || s.youtubeScraper == nil {
		return false
	}
	return s.youtubeScraper.SetProxyEnabled(enabled)
}

func (s *ScraperService) YouTubeProxyEnabled() bool {
	if s == nil || s.youtubeScraper == nil {
		return false
	}
	return s.youtubeScraper.ProxyEnabled()
}

func (s *ScraperService) ValidateStructure(ctx context.Context) error {
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

func (s *ScraperService) GetRecentVideos(ctx context.Context, channelID string, maxResults int) ([]*scraper.Video, error) {
	if s.youtubeScraper == nil {
		return nil, fmt.Errorf("youtube scraper not initialized")
	}

	videos, err := s.youtubeScraper.GetRecentVideos(ctx, channelID, maxResults)
	if err != nil {
		return nil, fmt.Errorf("youtube recent videos scraper error: %w", err)
	}

	s.logger.Debug("Recent videos fetched via scraper",
		slog.String("channel", channelID),
		slog.Int("count", len(videos)))

	return videos, nil
}

func (s *ScraperService) GetChannelStats(ctx context.Context, channelID string) (*scraper.ChannelStats, error) {
	if s.youtubeScraper == nil {
		return nil, fmt.Errorf("youtube scraper not initialized")
	}

	stats, err := s.youtubeScraper.GetChannelStats(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("youtube channel stats scraper error: %w", err)
	}

	s.logger.Debug("Channel stats fetched via scraper",
		slog.String("channel", channelID),
		slog.Int64("subscribers", stats.SubscriberCount))

	return stats, nil
}

func (s *ScraperService) GetChannelSnippet(ctx context.Context, channelID string) (*scraper.ChannelSnippet, error) {
	if s.youtubeScraper == nil {
		return nil, fmt.Errorf("youtube scraper not initialized")
	}

	snippet, err := s.youtubeScraper.GetChannelSnippet(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("youtube channel snippet scraper error: %w", err)
	}

	s.logger.Debug("Channel snippet fetched via scraper",
		slog.String("channel", channelID),
		slog.Int("avatars", len(snippet.Avatar)),
		slog.Int("banners", len(snippet.Banner)))

	return snippet, nil
}

func (s *ScraperService) GetPopularVideos(ctx context.Context, channelID string, maxResults int) ([]*scraper.Video, error) {
	if s.youtubeScraper == nil {
		return nil, fmt.Errorf("youtube scraper not initialized")
	}

	videos, err := s.youtubeScraper.GetPopularVideos(ctx, channelID, maxResults)
	if err != nil {
		return nil, fmt.Errorf("youtube popular videos scraper error: %w", err)
	}

	s.logger.Debug("Popular videos fetched via scraper",
		slog.String("channel", channelID),
		slog.Int("count", len(videos)))

	return videos, nil
}
