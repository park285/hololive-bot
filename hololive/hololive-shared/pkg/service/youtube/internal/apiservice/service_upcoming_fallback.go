package apiservice

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
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func (ys *serviceImpl) completeUpcomingAPIFallback(ctx context.Context, cacheKey string, scrapeResult upcomingScrapeResult) ([]*domain.Stream, error) {
	allStreams := scrapeResult.streams
	if len(scrapeResult.failedIDs) == 0 {
		ys.recordUpcomingFallbackSkipped(ctx)
		return allStreams, nil
	}

	estimatedCost := len(scrapeResult.failedIDs) * constants.YouTubeConfig.SearchQuotaCost
	quotaErr := ys.checkQuota(estimatedCost)
	secondary, err := ys.runUpcomingAPIFallback(ctx, scrapeResult, estimatedCost, quotaErr, &allStreams)
	if err != nil {
		return nil, fmt.Errorf("get upcoming streams: api fallback execution: %w", err)
	}
	if handled, streams, err := ys.handleUpcomingFallbackBlocked(ctx, cacheKey, allStreams, secondary, quotaErr); handled {
		return streams, err
	}
	if shouldReturnFallbackError(len(allStreams), len(scrapeResult.failedIDs), secondary.Result.Successes) {
		return nil, fmt.Errorf("get upcoming streams: scraper and api fallback failed for %d channels", len(scrapeResult.failedIDs))
	}
	return allStreams, nil
}

func (ys *serviceImpl) recordUpcomingFallbackSkipped(ctx context.Context) {
	_, _ = fallback.RunSecondary(ctx, fallback.SecondaryPlan{
		Service:   "youtube",
		Operation: "upcoming_streams",
		Trigger:   fallback.TriggerOnFailures,
		ShouldRun: false,
	})
}

func (ys *serviceImpl) runUpcomingAPIFallback(
	ctx context.Context,
	scrapeResult upcomingScrapeResult,
	estimatedCost int,
	quotaErr error,
	allStreams *[]*domain.Stream,
) (fallback.SecondaryExecution, error) {
	return fallback.RunSecondary(ctx, fallback.SecondaryPlan{
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
			*allStreams = append(*allStreams, apiResult.streams...)
			ys.consumeQuota(apiResult.quotaCost)
			ys.observeUpcomingFallbackRecoveries(scrapeResult, apiResult)

			return fallback.SecondaryResult{
				Items:     len(apiResult.streams),
				Successes: apiResult.successfulChannels,
			}, nil
		},
	})
}

func (ys *serviceImpl) handleUpcomingFallbackBlocked(
	ctx context.Context,
	cacheKey string,
	allStreams []*domain.Stream,
	secondary fallback.SecondaryExecution,
	quotaErr error,
) (bool, []*domain.Stream, error) {
	if secondary.Outcome != "blocked" {
		return false, nil, nil
	}
	ys.logger.Warn("Quota exceeded for API fallback, returning partial results",
		slog.Int("scraped_count", len(allStreams)),
		slog.Any("error", quotaErr))
	if len(allStreams) > 0 {
		ys.cache.SetStreams(ctx, cacheKey, allStreams, constants.YouTubeConfig.CacheExpiration)
		return true, allStreams, nil
	}
	return true, nil, fmt.Errorf("get upcoming streams: api fallback blocked after scraper failures: %w", quotaErr)
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
		g.Go(func() error {
			streams, err := ys.getChannelUpcomingStreams(gctx, channelID)
			if err != nil {
				detail := classifyYouTubeAPIFailure(err)
				ys.logger.Warn("Failed to fetch channel from API",
					slog.String("channelID", channelID),
					slog.String("source", string(scraper.FailureSourceAPI)),
					slog.String("reason", string(detail.Reason)),
					slog.Int("statusCode", detail.StatusCode),
					slog.Any("error", err))
				mu.Lock()
				result.failedIDs = append(result.failedIDs, channelID)
				result.failures = append(result.failures, upcomingScrapeFailure{
					ChannelID:  channelID,
					Source:     string(scraper.FailureSourceAPI),
					Reason:     string(detail.Reason),
					StatusCode: detail.StatusCode,
					RetryAfter: detail.RetryAfter,
					Message:    detail.Message,
				})
				mu.Unlock()
				return nil
			}

			mu.Lock()
			result.streams = append(result.streams, streams...)
			result.successfulChannels++
			result.successfulIDs = append(result.successfulIDs, channelID)
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

func (ys *serviceImpl) observeUpcomingFallbackRecoveries(scrapeResult upcomingScrapeResult, apiResult upcomingAPIFallbackResult) {
	failuresByChannel := upcomingFailureByChannel(scrapeResult.failures)
	for _, channelID := range apiResult.successfulIDs {
		failure, ok := failuresByChannel[channelID]
		if !ok {
			continue
		}
		observeYouTubeProducerScrapeRecovery("upcoming_streams", failure.Source, failure.Reason, string(scraper.FailureSourceAPI))
		ys.logger.Info("youtube_upcoming_api_fallback_recovered_channel",
			slog.String("channelID", channelID),
			slog.String("failedSource", failure.Source),
			slog.String("failedReason", failure.Reason),
			slog.String("recoverySource", string(scraper.FailureSourceAPI)))
	}
	for _, failure := range apiResult.failures {
		ys.logger.Warn("youtube_upcoming_api_fallback_unrecovered_channel",
			slog.String("channelID", failure.ChannelID),
			slog.String("source", failure.Source),
			slog.String("reason", failure.Reason),
			slog.Int("statusCode", failure.StatusCode))
	}
}

func (ys *serviceImpl) getChannelUpcomingStreams(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	response, err := ys.fetchChannelUpcomingSearch(ctx, channelID)
	if err != nil {
		return nil, err
	}

	streams := make([]*domain.Stream, 0, len(response.Items))
	for _, item := range response.Items {
		if stream := buildUpcomingAPIStream(channelID, item); stream != nil {
			streams = append(streams, stream)
		}
	}

	return streams, nil
}

func (ys *serviceImpl) fetchChannelUpcomingSearch(ctx context.Context, channelID string) (*youtube.SearchListResponse, error) {
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
	return response, nil
}

func buildUpcomingAPIStream(channelID string, item *youtube.SearchResult) *domain.Stream {
	if item.Id == nil || item.Id.VideoId == "" {
		return nil
	}

	stream := &domain.Stream{
		ID:        item.Id.VideoId,
		Title:     item.Snippet.Title,
		ChannelID: channelID,
		Status:    domain.StreamStatusUpcoming,
		Link:      new(fmt.Sprintf("https://www.youtube.com/watch?v=%s", item.Id.VideoId)),
		Thumbnail: extractThumbnail(item.Snippet.Thumbnails),
	}
	applyUpcomingAPIStreamPublishedAt(stream, item.Snippet.PublishedAt)
	applyUpcomingAPIStreamChannel(stream, channelID, item.Snippet.ChannelTitle)
	return stream
}

func applyUpcomingAPIStreamPublishedAt(stream *domain.Stream, publishedAt string) {
	if publishedAt == "" {
		return
	}
	if startTime, err := time.Parse(time.RFC3339, publishedAt); err == nil {
		stream.StartScheduled = &startTime
	}
}

func applyUpcomingAPIStreamChannel(stream *domain.Stream, channelID string, channelTitle string) {
	if channelTitle == "" {
		return
	}
	stream.Channel = &domain.Channel{
		ID:   channelID,
		Name: channelTitle,
	}
}

func extractThumbnail(thumbnails *youtube.ThumbnailDetails) *string {
	if thumbnails == nil {
		return nil
	}

	candidates := []*youtube.Thumbnail{
		thumbnails.Maxres,
		thumbnails.High,
		thumbnails.Medium,
		thumbnails.Default,
	}
	for _, thumbnail := range candidates {
		if thumbnail != nil && thumbnail.Url != "" {
			return &thumbnail.Url
		}
	}

	return nil
}
