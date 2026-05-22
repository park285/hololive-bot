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

package polling

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type VideosPoller struct {
	client     *scraper.Client
	db         *gorm.DB
	repository       batchRepository
	maxResults int
}

func NewVideosPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int) *VideosPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	return &VideosPoller{
		client:     scraperClient,
		db:         db,
		repository:       newBatchRepository(db),
		maxResults: maxResults,
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

	watermark, isInitialized := p.videoWatermark(ctx, channelID)
	newVideos := videosAfterWatermark(videos, watermark.LastContentID, isInitialized)
	dbVideos, notifications := buildVideoPollResults(channelID, newVideos, isInitialized)

	if err := p.persistVideoPollResults(ctx, channelID, videos[0].VideoID, dbVideos, notifications); err != nil {
		return fmt.Errorf("persist video batch: %w", err)
	}

	return nil
}

func (p *VideosPoller) videoWatermark(ctx context.Context, channelID string) (domain.YouTubeContentWatermark, bool) {
	var watermark domain.YouTubeContentWatermark
	err := p.db.WithContext(ctx).Where(
		"channel_id = ? AND watermark_type = ?",
		channelID, domain.WatermarkTypeVideo,
	).First(&watermark).Error

	isInitialized := err == nil && watermark.Initialized
	return watermark, isInitialized
}

func videosAfterWatermark(videos []*scraper.Video, lastSeenID string, isInitialized bool) []*scraper.Video {
	newVideos := make([]*scraper.Video, 0, len(videos))
	for _, video := range videos {
		if isInitialized && video.VideoID == lastSeenID {
			break
		}
		newVideos = append(newVideos, video)
	}
	return newVideos
}

func buildVideoPollResults(
	channelID string,
	newVideos []*scraper.Video,
	isInitialized bool,
) ([]*domain.YouTubeVideo, []*domain.YouTubeNotificationOutbox) {
	dbVideos := make([]*domain.YouTubeVideo, 0, len(newVideos))
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, len(newVideos))
	for _, video := range newVideos {
		isLiveReplay := isLiveReplayVideo(video.PublishedText)
		dbVideo := buildYouTubeVideo(channelID, video, isLiveReplay)
		dbVideos = append(dbVideos, dbVideo)

		if isInitialized && !isLiveReplay {
			notifications = append(notifications, buildVideoNotification(channelID, dbVideo))
		}
	}
	return dbVideos, notifications
}

func buildYouTubeVideo(channelID string, video *scraper.Video, isLiveReplay bool) *domain.YouTubeVideo {
	return &domain.YouTubeVideo{
		VideoID:       video.VideoID,
		ChannelID:     channelID,
		Title:         video.Title,
		Thumbnail:     convertThumbnails(video.Thumbnail),
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
		Payload:   mustMarshalJSON(video),
		Status:    domain.OutboxStatusPending,
	}
}

func (p *VideosPoller) persistVideoPollResults(
	ctx context.Context,
	channelID string,
	lastContentID string,
	dbVideos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
) error {
	return p.repository.PersistVideos(ctx, dbVideos, notifications, nil, &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: lastContentID,
	})
}
