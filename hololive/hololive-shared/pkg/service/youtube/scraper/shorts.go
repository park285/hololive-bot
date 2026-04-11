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

package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	"github.com/tidwall/gjson"
)

const shortsPublishedAtLookupWindow = 30

// GetShorts: 채널의 쇼츠 비디오 목록 조회 (/channel/{id}/shorts)
func (c *Client) GetShorts(ctx context.Context, channelID string, maxResults int) ([]*Short, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s/shorts", channelID)
	html, err := c.fetchPage(ctx, url)
	if err != nil {
		return nil, err
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
	}

	data := gjson.Parse(jsonStr)
	if err := checkAlerts(data); err != nil {
		return nil, err
	}

	// Shorts 탭 찾기
	tabPath := "contents.twoColumnBrowseResultsRenderer.tabs"
	var shortItems []gjson.Result

	data.Get(tabPath).ForEach(func(_, tab gjson.Result) bool {
		tabTitle := tab.Get("tabRenderer.title").String()
		if tabTitle == "Shorts" {
			richGridContents := tab.Get("tabRenderer.content.richGridRenderer.contents")
			richGridContents.ForEach(func(_, item gjson.Result) bool {
				shortsRenderer := item.Get("richItemRenderer.content.shortsLockupViewModel")
				if shortsRenderer.Exists() {
					shortItems = append(shortItems, shortsRenderer)
				}
				return true
			})
			return false
		}
		return true
	})

	shorts := make([]*Short, 0, min(len(shortItems), maxResults))
	for i, item := range shortItems {
		if i >= maxResults {
			break
		}
		short := c.parseShortsLockupViewModel(item)
		if short != nil {
			shorts = append(shorts, short)
		}
	}

	c.enrichShortsPublishedAt(ctx, channelID, shorts)

	return shorts, nil
}

func (c *Client) enrichShortsPublishedAt(ctx context.Context, channelID string, shorts []*Short) {
	if len(shorts) == 0 {
		return
	}

	lookupLimit := max(len(shorts)*3, shortsPublishedAtLookupWindow)
	videos, err := c.getRecentVideosFromRSS(ctx, channelID, lookupLimit)
	if err != nil {
		slog.Debug("shorts published_at rss lookup failed",
			"channel_id", channelID,
			"error", err.Error())
		return
	}

	if len(videos) == 0 {
		return
	}

	publishedAtByVideoID := make(map[string]*time.Time, len(videos))
	for _, video := range videos {
		if video == nil || video.VideoID == "" {
			continue
		}
		publishedAt, ok := yttimestamp.ParsePublishedAt(video.PublishedText)
		if !ok {
			continue
		}
		publishedAtByVideoID[video.VideoID] = publishedAt
	}

	for _, short := range shorts {
		short.PublishedAt = yttimestamp.NormalizePtr(publishedAtByVideoID[short.VideoID])
	}
}

// parseShortsLockupViewModel: shortsLockupViewModel JSON을 Short 구조체로 변환
func (c *Client) parseShortsLockupViewModel(short gjson.Result) *Short {
	videoID := short.Get("onTap.innertubeCommand.reelWatchEndpoint.videoId").String()
	if videoID == "" {
		return nil
	}

	var thumbnails []Thumbnail
	short.Get("thumbnail.sources").ForEach(func(_, t gjson.Result) bool {
		thumbnails = append(thumbnails, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})

	viewCountText := short.Get("overlayMetadata.secondaryText.content").String()
	viewCount := parseShortNumber(viewCountText)

	return &Short{
		VideoID:   videoID,
		Title:     short.Get("overlayMetadata.primaryText.content").String(),
		Thumbnail: thumbnails,
		ViewCount: viewCount,
	}
}
