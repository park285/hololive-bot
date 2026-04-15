package youtube

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/youtube/v3"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/fallback"
)

func (ys *serviceImpl) completeUpcomingAPIFallback(ctx context.Context, cacheKey string, scrapeResult upcomingScrapeResult) ([]*domain.Stream, error) {
	allStreams := scrapeResult.streams
	if len(scrapeResult.failedIDs) == 0 {
		_, _ = fallback.RunSecondary(ctx, fallback.SecondaryPlan{
			Service:   "youtube",
			Operation: "upcoming_streams",
			Trigger:   fallback.TriggerOnFailures,
			ShouldRun: false,
		})
		return allStreams, nil
	}

	estimatedCost := len(scrapeResult.failedIDs) * constants.YouTubeConfig.SearchQuotaCost
	quotaErr := ys.checkQuota(estimatedCost)
	secondary, err := fallback.RunSecondary(ctx, fallback.SecondaryPlan{
		Service:   "youtube",
		Operation: "upcoming_streams",
		Trigger:   fallback.TriggerOnFailures,
		ShouldRun: true,
		Blocked:   quotaErr != nil,
		Run: func(runCtx context.Context) (fallback.SecondaryResult, error) {
			ys.logger.Info("Fetching from YouTube API (fallback for failed scrapers)",
				slog.Int("channels", len(scrapeResult.failedIDs)),
				slog.Int("estimatedCost", estimatedCost))

			apiResult := ys.fetchUpcomingFromAPI(runCtx, scrapeResult.failedIDs)
			allStreams = append(allStreams, apiResult.streams...)
			ys.consumeQuota(apiResult.quotaCost)

			return fallback.SecondaryResult{
				Items:     len(apiResult.streams),
				Successes: apiResult.successfulChannels,
			}, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get upcoming streams: api fallback execution: %w", err)
	}
	if secondary.Outcome == "blocked" {
		ys.logger.Warn("Quota exceeded for API fallback, returning partial results",
			slog.Int("scraped_count", len(allStreams)),
			slog.Any("error", quotaErr))
		if len(allStreams) > 0 {
			ys.cache.SetStreams(ctx, cacheKey, allStreams, constants.YouTubeConfig.CacheExpiration)
			return allStreams, nil
		}
		return nil, fmt.Errorf("get upcoming streams: api fallback blocked after scraper failures: %w", quotaErr)
	}
	if shouldReturnFallbackError(len(allStreams), len(scrapeResult.failedIDs), secondary.Result.Successes) {
		return nil, fmt.Errorf("get upcoming streams: scraper and api fallback failed for %d channels", len(scrapeResult.failedIDs))
	}
	return allStreams, nil
}

func (ys *serviceImpl) fetchUpcomingFromAPI(ctx context.Context, channelIDs []string) upcomingAPIFallbackResult {
	result := upcomingAPIFallbackResult{
		streams: make([]*domain.Stream, 0, len(channelIDs)),
	}
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(constants.YouTubeConfig.MaxConcurrentRequests)

	var costMu sync.Mutex

	for _, channelID := range channelIDs {
		channelID := channelID
		g.Go(func() error {
			streams, err := ys.getChannelUpcomingStreams(gctx, channelID)
			if err != nil {
				ys.logger.Warn("Failed to fetch channel from API",
					slog.String("channelID", channelID),
					slog.Any("error", err))
				return nil
			}

			mu.Lock()
			result.streams = append(result.streams, streams...)
			result.successfulChannels++
			mu.Unlock()

			costMu.Lock()
			result.quotaCost += constants.YouTubeConfig.SearchQuotaCost
			costMu.Unlock()

			return nil
		})
	}

	_ = g.Wait()

	return result
}

func (ys *serviceImpl) getChannelUpcomingStreams(ctx context.Context, channelID string) ([]*domain.Stream, error) {
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
