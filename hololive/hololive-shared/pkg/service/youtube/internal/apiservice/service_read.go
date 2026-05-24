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

package apiservice

import (
	"context"
	"log/slog"

	sharedlog "github.com/park285/shared-go/pkg/logging"

	"google.golang.org/api/youtube/v3"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// 스크래퍼를 우선 사용하고, 실패 시 YouTube API로 폴백합니다.
func (ys *serviceImpl) GetRecentVideos(ctx context.Context, channelID string, maxResults int64) ([]string, error) {
	videos, err := ys.scraper.GetRecentVideos(ctx, channelID, int(maxResults))
	if hasScrapedRecentVideos(videos, err) {
		videoIDs := recentScraperVideoIDs(videos)
		ys.logger.Debug("Recent videos fetched via scraper",
			slog.String("channel", channelID),
			slog.Int("count", len(videoIDs)))
		return videoIDs, nil
	}

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
		return nil, sharedlog.LogAndWrapError(ctx, ys.logger, "YouTube search error", err, slog.String("channel", channelID))
	}

	videoIDs := recentAPIResponseVideoIDs(response.Items)

	ys.consumeQuota(constants.YouTubeConfig.SearchQuotaCost)

	ys.logger.Debug("Recent videos fetched via API",
		slog.String("channel", channelID),
		slog.Int("count", len(videoIDs)))

	return videoIDs, nil
}

func hasScrapedRecentVideos(videos []*scraper.Video, err error) bool {
	return err == nil && len(videos) > 0
}

func recentScraperVideoIDs(videos []*scraper.Video) []string {
	videoIDs := make([]string, 0, len(videos))
	for _, v := range videos {
		videoIDs = append(videoIDs, v.VideoID)
	}
	return videoIDs
}

func recentAPIResponseVideoIDs(items []*youtube.SearchResult) []string {
	videoIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.Id != nil && item.Id.VideoId != "" {
			videoIDs = append(videoIDs, item.Id.VideoId)
		}
	}
	return videoIDs
}
