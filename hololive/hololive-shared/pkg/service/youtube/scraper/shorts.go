package scraper

import (
	"context"
	"fmt"

	"github.com/tidwall/gjson"
)

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

	return shorts, nil
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
