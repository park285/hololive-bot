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

	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

type Service struct {
	httpClient           *http.Client
	identityIndex        officialidentity.Index
	logger               *slog.Logger
	officialSchedule     settings.OfficialScheduleConfig
	maxResponseBodyBytes int64
	youtubeClient        YouTubeClient
	officialPageMu       sync.RWMutex
	officialPage         officialSchedulePageCache
	officialGroup        singleflight.Group
	nowFunc              func() time.Time
}

const (
	officialScheduleCacheKey = "official_schedule_api:list:2"
	contentTypeJSON          = "application/json"
)

type officialSchedulePageCache struct {
	streams   []*domain.Stream
	expiresAt time.Time
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

func (s *Service) FetchUpcomingStreams(ctx context.Context, hours int) ([]*domain.Stream, error) {
	streams, err := s.fetchAllStreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch all streams: %w", err)
	}

	return filterScheduleWindow(streams, hours, false, s.now()), nil
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
	_, ok := errors.AsType[*StructureChangedError](err)
	return ok
}

func (s *Service) GetRecentVideos(ctx context.Context, channelID string, maxResults int) ([]*parser.Video, error) {
	if s.youtubeClient == nil {
		return nil, errors.New("youtube producer not initialized")
	}

	videos, err := s.youtubeClient.GetRecentVideos(ctx, channelID, maxResults)
	if err != nil {
		return nil, fmt.Errorf("youtube recent videos scraper error: %w", err)
	}

	s.logger.Debug("Recent videos fetched via scraper", slog.String("channel", channelID), slog.Int("count", len(videos)))

	return videos, nil
}

func (s *Service) GetPopularVideos(ctx context.Context, channelID string, maxResults int) ([]*parser.Video, error) {
	if s.youtubeClient == nil {
		return nil, errors.New("youtube producer not initialized")
	}

	videos, err := s.youtubeClient.GetPopularVideos(ctx, channelID, maxResults)
	if err != nil {
		return nil, fmt.Errorf("youtube popular videos scraper error: %w", err)
	}

	s.logger.Debug("Popular videos fetched via scraper", slog.String("channel", channelID), slog.Int("count", len(videos)))

	return videos, nil
}
