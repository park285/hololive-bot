package scraper

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// GetUpcomingEvents: 예정/라이브 방송 목록 조회
func (c *Client) GetUpcomingEvents(ctx context.Context, channelID string) ([]*UpcomingEvent, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)

	html, err := c.fetchPage(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
	}

	data := gjson.Parse(jsonStr)
	return parseUpcomingEventsFromInitialData(data)
}

// GetRecentVideos: 채널의 최근 업로드 비디오 목록 조회 (/channel/{id}/videos)
func (c *Client) GetRecentVideos(ctx context.Context, channelID string, maxResults int) ([]*Video, error) {
	if maxResults <= 0 {
		return []*Video{}, nil
	}

	if c.isVideoRSSBackoff(ctx, channelID) {
		rssVideos, rssErr := c.getRecentVideosFromRSS(ctx, channelID, maxResults)
		if rssErr == nil && len(rssVideos) > 0 {
			return rssVideos, nil
		}
		if rssErr == nil {
			slog.Debug("video rss backoff returned no videos, retrying html scraping",
				"channel_id", channelID)
			c.clearVideoRSSBackoff(ctx, channelID)
		}

		if rssErr != nil {
			slog.Warn("video rss backoff path failed, retrying html scraping",
				"channel_id", channelID,
				"error", rssErr.Error())
		}
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

	// 마지막 폴백: 채널 RSS feed. HTML 파싱 실패 시에도 최근 업로드 ID를 복구한다.
	rssVideos, rssErr := c.getRecentVideosFromRSS(ctx, channelID, maxResults)
	if rssErr != nil {
		slog.Debug("recent videos rss fallback failed",
			"channel_id", channelID,
			"error", rssErr.Error())
		return videos, nil
	}
	if len(rssVideos) > 0 {
		slog.Debug("recent videos recovered via rss fallback",
			"channel_id", channelID,
			"count", len(rssVideos))
		return rssVideos, nil
	}

	return []*Video{}, nil
}

func (c *Client) getRecentVideosFromPage(ctx context.Context, pageURL, channelID string, maxResults int) ([]*Video, error) {
	html, err := c.fetchPage(ctx, pageURL)
	if err != nil {
		return nil, err
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
	}

	data := gjson.Parse(jsonStr)
	return parseVideosFromInitialData(data, channelID, maxResults, c.parseVideoRenderer)
}

func (c *Client) getRecentVideosFromRSS(ctx context.Context, channelID string, maxResults int) ([]*Video, error) {
	if maxResults <= 0 {
		return []*Video{}, nil
	}

	rssURL := fmt.Sprintf("https://www.youtube.com/feeds/videos.xml?channel_id=%s", channelID)
	html, err := c.fetchPage(ctx, rssURL)
	if err != nil {
		return nil, err
	}
	videos, err := parseVideosFromRSSFeed(html, channelID, maxResults)
	if err != nil {
		return nil, err
	}
	return videos, nil
}

type rssFeed struct {
	Entries []rssEntry `xml:"entry"`
}

type rssEntry struct {
	VideoID     string        `xml:"http://www.youtube.com/xml/schemas/2015 videoId"`
	Title       string        `xml:"title"`
	Published   string        `xml:"published"`
	AuthorName  string        `xml:"author>name"`
	MediaGroup  rssMediaGroup `xml:"http://search.yahoo.com/mrss/ group"`
	MediaThumbs []rssThumb    `xml:"http://search.yahoo.com/mrss/ thumbnail"`
}

type rssMediaGroup struct {
	Thumbnails []rssThumb `xml:"http://search.yahoo.com/mrss/ thumbnail"`
}

type rssThumb struct {
	URL    string `xml:"url,attr"`
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
}

func parseVideosFromRSSFeed(feedXML, channelID string, maxResults int) ([]*Video, error) {
	if strings.TrimSpace(feedXML) == "" {
		return []*Video{}, nil
	}
	if maxResults <= 0 {
		return []*Video{}, nil
	}

	var feed rssFeed
	if err := xml.Unmarshal([]byte(feedXML), &feed); err != nil {
		return nil, fmt.Errorf("parse rss feed xml: %w", err)
	}

	videos := make([]*Video, 0, min(len(feed.Entries), maxResults))
	seen := make(map[string]struct{}, maxResults)

	for _, entry := range feed.Entries {
		if len(videos) >= maxResults {
			break
		}
		videoID := strings.TrimSpace(entry.VideoID)
		title := strings.TrimSpace(entry.Title)
		if videoID == "" || title == "" {
			continue
		}
		if _, exists := seen[videoID]; exists {
			continue
		}
		seen[videoID] = struct{}{}

		publishedText := strings.TrimSpace(entry.Published)
		if parsed, err := time.Parse(time.RFC3339, publishedText); err == nil {
			publishedText = parsed.UTC().Format(time.RFC3339)
		}

		thumbnails := entry.MediaGroup.Thumbnails
		if len(thumbnails) == 0 && len(entry.MediaThumbs) > 0 {
			thumbnails = entry.MediaThumbs
		}
		convertedThumbs := make([]Thumbnail, 0, len(thumbnails))
		for _, thumb := range thumbnails {
			if strings.TrimSpace(thumb.URL) == "" {
				continue
			}
			convertedThumbs = append(convertedThumbs, Thumbnail(thumb))
		}

		videos = append(videos, &Video{
			VideoID:       videoID,
			Title:         title,
			Thumbnail:     convertedThumbs,
			PublishedText: publishedText,
			ChannelID:     channelID,
			ChannelTitle:  strings.TrimSpace(entry.AuthorName),
		})
	}

	return videos, nil
}

// GetPopularVideos: 채널의 인기 비디오 목록 조회 (Home 탭의 Popular 섹션)
func (c *Client) GetPopularVideos(ctx context.Context, channelID string, maxResults int) ([]*Video, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)
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

	// Home 탭에서 "Popular" shelf 찾기
	sectionsPath := "contents.twoColumnBrowseResultsRenderer.tabs.0.tabRenderer.content.sectionListRenderer.contents"
	var popularItems []gjson.Result

	data.Get(sectionsPath).ForEach(func(_, section gjson.Result) bool {
		shelfTitle := section.Get("itemSectionRenderer.contents.0.shelfRenderer.title.runs.0.text").String()
		if shelfTitle == "Popular videos" || shelfTitle == "Popular" {
			gridItems := section.Get("itemSectionRenderer.contents.0.shelfRenderer.content.gridRenderer.items")
			gridItems.ForEach(func(_, item gjson.Result) bool {
				if item.Get("gridVideoRenderer").Exists() {
					popularItems = append(popularItems, item.Get("gridVideoRenderer"))
				}
				return true
			})
			return false
		}
		return true
	})

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

	return videos, nil
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
