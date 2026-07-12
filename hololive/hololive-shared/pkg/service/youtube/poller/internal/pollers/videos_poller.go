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

package pollers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/batchrepo"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type VideosPoller struct {
	client     *scraper.Client
	db         pollerDB
	repository batchrepo.BatchRepository
	maxResults int
	deferrals  *freshnessDeferrals
}

func NewVideosPoller(scraperClient *scraper.Client, db any, maxResults int) *VideosPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	querier := normalizePollerDB(db)
	return &VideosPoller{
		client:     scraperClient,
		db:         querier,
		repository: batchrepo.NewPgxBatchRepositoryWithPersister(querier, newDeliveryTelemetryLatencyPersisterAdapter(querier)),
		maxResults: maxResults,
		deferrals:  newFreshnessDeferrals(),
	}
}

func (p *VideosPoller) Name() string {
	return "videos"
}

func (p *VideosPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *VideosPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *VideosPoller) Poll(ctx context.Context, channelID string) error {
	videos, err := p.client.GetRecentVideos(ctx, channelID, p.maxResults)
	if err != nil {
		return fmt.Errorf("failed to get recent videos: %w", err)
	}

	if len(videos) == 0 {
		return nil
	}

	watermark, isInitialized, err := loadContentWatermark(ctx, p.db, channelID, domain.WatermarkTypeVideo)
	if err != nil {
		return err
	}
	detectedAt := time.Now().UTC()
	newVideos, watermarkFound := videosAfterWatermark(videos, watermark.LastContentID, isInitialized)
	logMissingVideoWatermark(ctx, channelID, len(videos), isInitialized, watermarkFound)
	knownVideoIDs, err := p.loadKnownVideoIDs(ctx, newVideos, isInitialized)
	if err != nil {
		return err
	}
	publishedAtByID := p.resolveVideoPublishedTimes(ctx, channelID, newVideos, knownVideoIDs, isInitialized, detectedAt)
	dbVideos, notifications, deferredVideoIDs := p.buildVideoPollResults(ctx, channelID, newVideos, knownVideoIDs, isInitialized, publishedAtByID, detectedAt)
	holdWatermark, departed := p.deferrals.reconcileChannel(channelID, collectedVideoIDSet(videos), deferredVideoIDs)
	for _, videoID := range departed {
		slog.WarnContext(ctx, "Video deferred candidate left the collected page; dropping without notification after max attempts",
			logschema.FieldChannelID, channelID,
			"video_id", videoID,
		)
	}
	var nextWatermark *domain.YouTubeContentWatermark
	if !holdWatermark {
		nextWatermark = &domain.YouTubeContentWatermark{
			ChannelID:     channelID,
			WatermarkType: domain.WatermarkTypeVideo,
			Initialized:   true,
			LastContentID: videos[0].VideoID,
		}
	} else {
		slog.WarnContext(ctx, "Video watermark held while freshness candidates stay deferred",
			logschema.FieldChannelID, channelID,
			"deferred_count", len(deferredVideoIDs),
		)
	}

	if err := p.persistVideoPollResults(ctx, dbVideos, notifications, nextWatermark); err != nil {
		return fmt.Errorf("persist video batch: %w", err)
	}

	return nil
}

func logMissingVideoWatermark(ctx context.Context, channelID string, collectedCount int, isInitialized, watermarkFound bool) {
	if !isInitialized || watermarkFound {
		return
	}

	slog.WarnContext(ctx, "Video watermark missing from collected page; classifying full page by publication freshness",
		logschema.FieldChannelID, channelID,
		"collected_count", collectedCount,
	)
}

func (p *VideosPoller) loadKnownVideoIDs(
	ctx context.Context,
	videos []*scraper.Video,
	isInitialized bool,
) (map[string]struct{}, error) {
	if !isInitialized {
		return nil, nil
	}
	return loadKnownVideoIDs(ctx, p.db, collectedVideoIDs(videos))
}

func (p *VideosPoller) resolveVideoPublishedTimes(
	ctx context.Context,
	channelID string,
	videos []*scraper.Video,
	knownVideoIDs map[string]struct{},
	isInitialized bool,
	now time.Time,
) map[string]time.Time {
	if !isInitialized || !videosNeedPublicationResolve(videos, knownVideoIDs, now) {
		return nil
	}
	publishedAtByID, err := p.client.GetRecentVideoPublishedTimes(ctx, channelID, p.maxResults)
	if err != nil {
		slog.WarnContext(ctx, "Video RSS published_at resolve failed",
			logschema.FieldChannelID, channelID,
			"error", err.Error(),
		)
		publishedAtByID = make(map[string]time.Time)
	}
	if publishedAtByID == nil {
		publishedAtByID = make(map[string]time.Time)
	}

	for _, video := range videos {
		if video == nil || polling.IsLiveReplayVideo(video.PublishedText) {
			continue
		}
		if _, known := knownVideoIDs[video.VideoID]; known {
			continue
		}
		if videoPublishedTextEvidence(video.PublishedText, now).freshness != videoFreshnessUnresolved {
			continue
		}
		if publishedAt, ok := publishedAtByID[video.VideoID]; ok && classifyVideoPublishedAt(publishedAt, now) != videoFreshnessUnresolved {
			continue
		}
		publishedAt, resolveErr := p.client.GetVideoPublishedAt(ctx, channelID, video.VideoID)
		if resolveErr != nil {
			slog.WarnContext(ctx, "Video watch published_at resolve failed",
				logschema.FieldChannelID, channelID,
				"video_id", video.VideoID,
				"error", resolveErr.Error(),
			)
			continue
		}
		if publishedAt != nil {
			publishedAtByID[video.VideoID] = publishedAt.UTC()
		}
	}
	return publishedAtByID
}

func videosNeedPublicationResolve(videos []*scraper.Video, knownVideoIDs map[string]struct{}, now time.Time) bool {
	for _, video := range videos {
		if video == nil || polling.IsLiveReplayVideo(video.PublishedText) {
			continue
		}
		if _, known := knownVideoIDs[video.VideoID]; known {
			continue
		}
		if videoPublishedTextEvidence(video.PublishedText, now).freshness == videoFreshnessUnresolved {
			return true
		}
	}
	return false
}

func videosAfterWatermark(videos []*scraper.Video, lastSeenID string, isInitialized bool) ([]*scraper.Video, bool) {
	newVideos := make([]*scraper.Video, 0, len(videos))
	for _, video := range videos {
		if isInitialized && video.VideoID == lastSeenID {
			return newVideos, true
		}
		newVideos = append(newVideos, video)
	}
	return newVideos, false
}

func (p *VideosPoller) buildVideoPollResults(
	ctx context.Context,
	channelID string,
	newVideos []*scraper.Video,
	knownVideoIDs map[string]struct{},
	isInitialized bool,
	publishedAtByID map[string]time.Time,
	now time.Time,
) ([]*domain.YouTubeVideo, []*domain.YouTubeNotificationOutbox, map[string]struct{}) {
	dbVideos := make([]*domain.YouTubeVideo, 0, len(newVideos))
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, len(newVideos))
	deferredVideoIDs := make(map[string]struct{})
	for _, video := range newVideos {
		isLiveReplay := polling.IsLiveReplayVideo(video.PublishedText)
		dbVideo := buildYouTubeVideo(channelID, video, isLiveReplay)
		if !isInitialized || isLiveReplay {
			dbVideos = append(dbVideos, dbVideo)
			continue
		}
		if _, known := knownVideoIDs[video.VideoID]; known {
			p.deferrals.clear(channelID, video.VideoID)
			dbVideos = append(dbVideos, dbVideo)
			continue
		}

		evidence := videoPublishedTextEvidence(video.PublishedText, now)
		if evidence.freshness == videoFreshnessUnresolved {
			if publishedAt, ok := publishedAtByID[video.VideoID]; ok {
				evidence = videoPublicationEvidence{
					freshness:   classifyVideoPublishedAt(publishedAt, now),
					publishedAt: &publishedAt,
				}
			}
		}
		if evidence.freshness == videoFreshnessUnresolved {
			attempts := p.deferrals.recordFailure(channelID, video.VideoID)
			if attempts < publicationFreshnessMaxAttempts {
				slog.WarnContext(ctx, "Video freshness unresolved; deferring candidate without notification",
					logschema.FieldChannelID, channelID,
					"video_id", video.VideoID,
					"attempts", attempts,
				)
				deferredVideoIDs[video.VideoID] = struct{}{}
				continue
			}
			p.deferrals.clear(channelID, video.VideoID)
			slog.WarnContext(ctx, "Video freshness unresolved after max attempts; absorbing silently without notification",
				logschema.FieldChannelID, channelID,
				"video_id", video.VideoID,
				"attempts", attempts,
			)
			dbVideos = append(dbVideos, dbVideo)
			continue
		}
		p.deferrals.clear(channelID, video.VideoID)
		dbVideo.PublishedAt = evidence.publishedAt
		dbVideos = append(dbVideos, dbVideo)
		if evidence.freshness == videoFreshnessFresh {
			notifications = append(notifications, buildVideoNotification(channelID, dbVideo))
		}
	}
	return dbVideos, notifications, deferredVideoIDs
}

func buildYouTubeVideo(channelID string, video *scraper.Video, isLiveReplay bool) *domain.YouTubeVideo {
	return &domain.YouTubeVideo{
		VideoID:       video.VideoID,
		ChannelID:     channelID,
		Title:         video.Title,
		Thumbnail:     polling.ConvertThumbnails(video.Thumbnail),
		Duration:      video.Duration,
		PublishedText: video.PublishedText,
		IsShort:       false,
		IsLiveReplay:  isLiveReplay,
		ViewCount:     video.ViewCount,
	}
}

func buildVideoNotification(channelID string, video *domain.YouTubeVideo) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindNewVideo,
		ChannelID: channelID,
		ContentID: video.VideoID,
		Payload:   polling.MustMarshalJSON(video),
		Status:    domain.OutboxStatusPending,
	}
}

func (p *VideosPoller) persistVideoPollResults(
	ctx context.Context,
	dbVideos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
	watermark *domain.YouTubeContentWatermark,
) error {
	return p.repository.PersistVideos(ctx, dbVideos, notifications, nil, watermark)
}
