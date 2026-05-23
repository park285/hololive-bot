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

package scraping

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/tidwall/gjson"
)

func (c *Client) GetUpcomingEvents(ctx context.Context, channelID string) ([]*UpcomingEvent, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)

	html, err := c.fetchChannelSourcePage(ctx, "upcoming_events", channelID, url, FailureSourceHTML)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		logStructureWarning("upcoming_events", channelID, "ytInitialData extraction failed", "error", err)
		return nil, c.recordParserDrift(ctx, "upcoming_events", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
	}

	data := gjson.Parse(jsonStr)
	events, err := parseUpcomingEventsFromInitialData(data)
	if err != nil {
		logStructureWarning("upcoming_events", channelID, "failed to parse initial data", "error", err)
		return nil, c.recordParserDrift(ctx, "upcoming_events", "parse_initial_data", channelID, url, FailureSourceHTML, html, err)
	}
	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
	return events, nil
}

func (c *Client) GetRecentVideos(ctx context.Context, channelID string, maxResults int) ([]*Video, error) {
	if maxResults <= 0 {
		return []*Video{}, nil
	}

	if rssVideos, ok := c.getRecentVideosFromRSSBackoff(ctx, channelID, maxResults); ok {
		return rssVideos, nil
	}

	url := fmt.Sprintf("https://www.youtube.com/channel/%s/videos", channelID)
	videos, err := c.getRecentVideosFromPage(ctx, url, channelID, maxResults)
	if err != nil {
		if !isRetryableVideoPageError(err) {
			return nil, err
		}
	} else {
		c.clearVideoRSSBackoff(ctx, channelID)
		return videos, nil
	}

	if isRetryableVideoPageError(err) {
		c.markVideoRSSBackoff(ctx, channelID)
	}

	if rssVideos, recovered := c.getRecentVideosFromRSSFallback(ctx, channelID, maxResults, videos); recovered {
		return rssVideos, nil
	}

	return []*Video{}, nil
}

func (c *Client) getRecentVideosFromRSSBackoff(ctx context.Context, channelID string, maxResults int) ([]*Video, bool) {
	if !c.isVideoRSSBackoff(ctx, channelID) {
		return nil, false
	}

	rssVideos, rssErr := c.getRecentVideosFromRSS(ctx, channelID, maxResults)
	if rssErr == nil && len(rssVideos) > 0 {
		return rssVideos, true
	}
	if rssErr == nil {
		slog.Debug("video rss backoff returned no videos, retrying html scraping",
			"channel_id", channelID)
		c.clearVideoRSSBackoff(ctx, channelID)
		return nil, false
	}

	slog.Warn("video rss backoff path failed, retrying html scraping",
		"channel_id", channelID,
		"error", rssErr.Error())
	return nil, false
}

func (c *Client) getRecentVideosFromRSSFallback(ctx context.Context, channelID string, maxResults int, pageVideos []*Video) ([]*Video, bool) {
	rssVideos, rssErr := c.getRecentVideosFromRSS(ctx, channelID, maxResults)
	if rssErr != nil {
		slog.Debug("recent videos rss fallback failed",
			"channel_id", channelID,
			"error", rssErr.Error())
		return pageVideos, true
	}
	if len(rssVideos) == 0 {
		return nil, false
	}

	logStructureWarning("recent_videos", channelID, "html parser recovered via rss fallback",
		"channel_id", channelID,
		"count", len(rssVideos))
	return rssVideos, true
}

func (c *Client) getRecentVideosFromPage(ctx context.Context, pageURL, channelID string, maxResults int) ([]*Video, error) {
	html, err := c.fetchChannelSourcePage(ctx, "recent_videos", channelID, pageURL, FailureSourceHTML)
	if err != nil {
		return nil, err
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		logStructureWarning("recent_videos", channelID, "ytInitialData extraction failed", "error", err)
		return nil, c.recordParserDrift(ctx, "recent_videos", "extract_yt_initial_data", channelID, pageURL, FailureSourceHTML, html, err)
	}

	data := gjson.Parse(jsonStr)
	videos, err := parseVideosFromInitialData(data, channelID, maxResults, c.parseVideoRenderer)
	if err != nil {
		logStructureWarning("recent_videos", channelID, "failed to parse initial data", "error", err)
		return nil, c.recordParserDrift(ctx, "recent_videos", "parse_initial_data", channelID, pageURL, FailureSourceHTML, html, err)
	}
	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
	return videos, nil
}

func (c *Client) getRecentVideosFromRSS(ctx context.Context, channelID string, maxResults int) ([]*Video, error) {
	if maxResults <= 0 {
		return []*Video{}, nil
	}

	rssURL := fmt.Sprintf("https://www.youtube.com/feeds/videos.xml?channel_id=%s", channelID)
	html, err := c.fetchChannelSourcePage(ctx, "recent_videos_rss", channelID, rssURL, FailureSourceRSS)
	if err != nil {
		return nil, err
	}
	videos, err := parseVideosFromRSSFeed(html, channelID, maxResults)
	if err != nil {
		return nil, c.recordParserDrift(ctx, "recent_videos_rss", "parse_rss_feed", channelID, rssURL, FailureSourceRSS, html, err)
	}
	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceRSS)
	return videos, nil
}

func (c *Client) GetPopularVideos(ctx context.Context, channelID string, maxResults int) ([]*Video, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)
	html, err := c.fetchChannelSourcePage(ctx, "popular_videos", channelID, url, FailureSourceHTML)
	if err != nil {
		return nil, err
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		return nil, c.recordParserDrift(ctx, "popular_videos", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
	}

	data := gjson.Parse(jsonStr)
	if err := checkAlerts(data); err != nil {
		return nil, err
	}

	popularItems := findPopularGridVideoRenderers(data)
	videos := c.parsePopularGridVideos(popularItems, channelID, maxResults)
	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
	return videos, nil
}

func findPopularGridVideoRenderers(data gjson.Result) []gjson.Result {
	var popularItems []gjson.Result
	popularSections(data).ForEach(func(_, section gjson.Result) bool {
		if !isPopularVideosShelf(section) {
			return true
		}
		popularItems = collectGridVideoRenderers(section)
		return false
	})
	return popularItems
}

func popularSections(data gjson.Result) gjson.Result {
	sectionsPath := "contents.twoColumnBrowseResultsRenderer.tabs.0.tabRenderer.content.sectionListRenderer.contents"
	return data.Get(sectionsPath)
}

func isPopularVideosShelf(section gjson.Result) bool {
	shelfTitle := section.Get("itemSectionRenderer.contents.0.shelfRenderer.title.runs.0.text").String()
	return shelfTitle == "Popular videos" || shelfTitle == "Popular"
}

func collectGridVideoRenderers(section gjson.Result) []gjson.Result {
	var gridVideos []gjson.Result
	gridItems := section.Get("itemSectionRenderer.contents.0.shelfRenderer.content.gridRenderer.items")
	gridItems.ForEach(func(_, item gjson.Result) bool {
		if item.Get("gridVideoRenderer").Exists() {
			gridVideos = append(gridVideos, item.Get("gridVideoRenderer"))
		}
		return true
	})
	return gridVideos
}

func (c *Client) parsePopularGridVideos(popularItems []gjson.Result, channelID string, maxResults int) []*Video {
	videos := make([]*Video, 0, min(len(popularItems), maxResults))
	for i, item := range popularItems {
		if i >= maxResults {
			break
		}
		video := c.parseGridVideoRenderer(item, channelID)
		if video != nil {
			videos = append(videos, video)
		}
	}

	return videos
}

// parseVideoCommon: videoRenderer/gridVideoRenderer 공통 파싱 로직
func (c *Client) parseVideoCommon(video gjson.Result, channelID, durationPath, channelTitlePath, channelHandlePath string) *Video {
	videoID := video.Get("videoId").String()
	if videoID == "" {
		return nil
	}

	var thumbnails []Thumbnail
	video.Get("thumbnail.thumbnails").ForEach(func(_, t gjson.Result) bool {
		thumbnails = append(thumbnails, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})

	title := video.Get("title.runs.0.text").String()
	if title == "" {
		title = video.Get("title.simpleText").String()
	}

	viewCountText := video.Get("viewCountText.simpleText").String()
	viewCount := parseViewCount(viewCountText)

	return &Video{
		VideoID:       videoID,
		Title:         title,
		Thumbnail:     thumbnails,
		ViewCount:     viewCount,
		PublishedText: video.Get("publishedTimeText.simpleText").String(),
		Duration:      video.Get(durationPath).String(),
		ChannelID:     channelID,
		ChannelTitle:  video.Get(channelTitlePath).String(),
		ChannelHandle: video.Get(channelHandlePath).String(),
	}
}

// parseVideoRenderer: videoRenderer JSON을 Video 구조체로 변환
func (c *Client) parseVideoRenderer(video gjson.Result, channelID string) *Video {
	return c.parseVideoCommon(
		video,
		channelID,
		"lengthText.simpleText",
		"ownerText.runs.0.text",
		"ownerText.runs.0.navigationEndpoint.browseEndpoint.canonicalBaseUrl",
	)
}

// parseGridVideoRenderer: gridVideoRenderer JSON을 Video 구조체로 변환
func (c *Client) parseGridVideoRenderer(video gjson.Result, channelID string) *Video {
	return c.parseVideoCommon(
		video,
		channelID,
		"thumbnailOverlays.0.thumbnailOverlayTimeStatusRenderer.text.simpleText",
		"shortBylineText.runs.0.text",
		"shortBylineText.runs.0.navigationEndpoint.browseEndpoint.canonicalBaseUrl",
	)
}

func parseLockupVideoViewModel(lockup gjson.Result, channelID string) *Video {
	if lockup.Get("contentType").String() != "LOCKUP_CONTENT_TYPE_VIDEO" {
		return nil
	}

	videoID := lockup.Get("contentId").String()
	if videoID == "" {
		videoID = lockup.Get("rendererContext.commandContext.onTap.innertubeCommand.watchEndpoint.videoId").String()
	}
	if videoID == "" {
		return nil
	}

	var thumbnails []Thumbnail
	lockup.Get("contentImage.thumbnailViewModel.image.sources").ForEach(func(_, t gjson.Result) bool {
		thumbnails = append(thumbnails, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})

	metadataParts := lockup.Get("metadata.lockupMetadataViewModel.metadata.contentMetadataViewModel.metadataRows.0.metadataParts")
	viewCount, publishedText := pickLockupMetadataTexts(metadataParts)

	return &Video{
		VideoID:       videoID,
		Title:         lockup.Get("metadata.lockupMetadataViewModel.title.content").String(),
		Thumbnail:     thumbnails,
		ViewCount:     viewCount,
		PublishedText: publishedText,
		Duration:      lockup.Get("contentImage.thumbnailViewModel.overlays.0.thumbnailBottomOverlayViewModel.badges.0.thumbnailBadgeViewModel.text").String(),
		ChannelID:     channelID,
	}
}

// pickLockupMetadataTexts: lockup metadataParts 항목의 순서 의존성을 없앤다.
// parseViewCount > 0인 항목을 viewCount로 식별하고, 나머지 첫 번째 항목을 published 텍스트로 사용.
// 모든 항목에서 viewCount를 식별하지 못하면 기존 동작(0=viewCount, 1=published)으로 폴백.
func pickLockupMetadataTexts(parts gjson.Result) (int64, string) {
	texts := collectLockupTexts(parts)
	if viewCount, published, ok := pickViewCountAndPublished(texts); ok {
		return viewCount, published
	}
	return fallbackPickMetadata(texts)
}

func collectLockupTexts(parts gjson.Result) []string {
	var texts []string
	parts.ForEach(func(_, part gjson.Result) bool {
		text := part.Get("text.content").String()
		if text != "" {
			texts = append(texts, text)
		}
		return true
	})
	return texts
}

func pickViewCountAndPublished(texts []string) (int64, string, bool) {
	for i, t := range texts {
		parsed := parseViewCount(t)
		if parsed <= 0 {
			continue
		}
		return parsed, firstOtherText(texts, i), true
	}
	return 0, "", false
}

func firstOtherText(texts []string, excludeIdx int) string {
	for i, t := range texts {
		if i == excludeIdx {
			continue
		}
		return t
	}
	return ""
}

func fallbackPickMetadata(texts []string) (int64, string) {
	var viewText, publishedText string
	if len(texts) > 0 {
		viewText = texts[0]
	}
	if len(texts) > 1 {
		publishedText = texts[1]
	}
	return parseViewCount(viewText), publishedText
}
