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

func checkAlerts(data gjson.Result) error {
	alerts := data.Get("alerts")
	if !alerts.Exists() {
		return nil
	}

	var foundAlert bool
	var selectedText string
	var selectedNotFound bool

	alerts.ForEach(func(_, alert gjson.Result) bool {
		alertType := alert.Get("alertRenderer.type").String()
		alertText := extractAlertText(alert)

		if alertType == "ERROR" {
			foundAlert = true
			if selectedText == "" {
				selectedText = alertText
			}
			lowerText := strings.ToLower(alertText)
			if strings.Contains(lowerText, "does not exist") ||
				strings.Contains(lowerText, "doesn't exist") ||
				strings.Contains(lowerText, "been terminated") {
				selectedNotFound = true
				if alertText != "" {
					selectedText = alertText
				}
				return false
			}
		}
		return true
	})

	if foundAlert {
		if selectedText == "" {
			selectedText = "unknown channel alert"
		}
		if selectedNotFound {
			return fmt.Errorf("%w: %s", ErrChannelNotFound, selectedText)
		}
		return fmt.Errorf("%w: %s", ErrChannelUnavailable, selectedText)
	}

	return nil
}

func extractAlertText(alert gjson.Result) string {
	alertText := strings.TrimSpace(alert.Get("alertRenderer.text.simpleText").String())
	if alertText != "" {
		return alertText
	}

	var parts []string
	alert.Get("alertRenderer.text.runs").ForEach(func(_, run gjson.Result) bool {
		text := strings.TrimSpace(run.Get("text").String())
		if text != "" {
			parts = append(parts, text)
		}
		return true
	})

	return strings.TrimSpace(strings.Join(parts, " "))
}

// GetUpcomingEvents: 예정/라이브 방송 목록 조회
//
//nolint:gocognit // 중첩된 JSON 구조 탐색으로 인한 복잡도
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
	if err := checkAlerts(data); err != nil {
		return nil, err
	}

	var events []*UpcomingEvent
	seen := make(map[string]bool)

	// tabs.0.tabRenderer.content.sectionListRenderer.contents를 순회
	sectionsPath := "contents.twoColumnBrowseResultsRenderer.tabs.0.tabRenderer.content.sectionListRenderer.contents"
	sections := data.Get(sectionsPath)

	sections.ForEach(func(_, section gjson.Result) bool {
		contents := section.Get("itemSectionRenderer.contents")

		contents.ForEach(func(_, content gjson.Result) bool {
			// channelFeaturedContentRenderer 확인 (Featured area)
			featuredItems := content.Get("channelFeaturedContentRenderer.items")
			featuredItems.ForEach(func(_, item gjson.Result) bool {
				video := item.Get("videoRenderer")
				if video.Exists() {
					event := parseVideoToEvent(video)
					if event != nil && !seen[event.VideoID] {
						if event.Status == "LIVE" || event.Status == "UPCOMING" {
							seen[event.VideoID] = true
							events = append(events, event)
						}
					}
				}
				return true
			})

			// shelfRenderer 확인 (기존 구조 호환)
			shelfItems := content.Get("shelfRenderer.content.horizontalListRenderer.items")
			shelfItems.ForEach(func(_, item gjson.Result) bool {
				video := item.Get("videoRenderer")
				if !video.Exists() {
					video = item.Get("gridVideoRenderer")
				}
				if video.Exists() {
					event := parseVideoToEvent(video)
					if event != nil && !seen[event.VideoID] {
						if event.Status == "LIVE" || event.Status == "UPCOMING" {
							seen[event.VideoID] = true
							events = append(events, event)
						}
					}
				}
				return true
			})

			return true
		})
		return true
	})

	return events, nil
}

// parseVideoToEvent: videoRenderer/gridVideoRenderer를 UpcomingEvent로 변환
func parseVideoToEvent(video gjson.Result) *UpcomingEvent {
	videoID := video.Get("videoId").String()
	if videoID == "" {
		return nil
	}

	// 상태 확인 (thumbnailOverlays에서 LIVE/UPCOMING 체크)
	status := "DEFAULT"
	overlays := video.Get("thumbnailOverlays")
	overlays.ForEach(func(_, overlay gjson.Result) bool {
		style := overlay.Get("thumbnailOverlayTimeStatusRenderer.style").String()
		if style == "LIVE" || style == "UPCOMING" {
			status = style
			return false // break
		}
		return true
	})

	// upcomingEventData도 체크
	if video.Get("upcomingEventData").Exists() && status == "DEFAULT" {
		status = "UPCOMING"
	}

	// 썸네일 파싱
	var thumbnails []Thumbnail
	video.Get("thumbnail.thumbnails").ForEach(func(_, t gjson.Result) bool {
		thumbnails = append(thumbnails, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})

	// 시작 시간
	var startTime *int64
	if st := video.Get("upcomingEventData.startTime").Int(); st > 0 {
		startTime = &st
	}

	// 제목 추출 (simpleText 또는 runs.0.text)
	title := video.Get("title.simpleText").String()
	if title == "" {
		title = video.Get("title.runs.0.text").String()
	}

	// 조회수 텍스트
	viewCountText := video.Get("viewCountText.simpleText").String()
	if viewCountText == "" {
		viewCountText = video.Get("viewCountText.runs.0.text").String()
	}

	return &UpcomingEvent{
		VideoID:       videoID,
		Title:         title,
		Thumbnail:     thumbnails,
		Status:        status,
		StartTime:     startTime,
		ViewCountText: viewCountText,
		ChannelTitle:  video.Get("shortBylineText.runs.0.text").String(),
	}
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

// parseVideosFromInitialData: ytInitialData JSON에서 비디오 목록을 파싱하는 순수 함수
func parseVideosFromInitialData(
	data gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(gjson.Result, string) *Video,
) ([]*Video, error) {
	if err := checkAlerts(data); err != nil {
		return nil, err
	}

	tabPath := "contents.twoColumnBrowseResultsRenderer.tabs"
	tabs := data.Get(tabPath)

	if !tabs.Exists() {
		hasContents := data.Get("contents").Exists()
		hasAlerts := data.Get("alerts").Exists()
		fallbackVideos := parseVideosFromContentsFallback(data.Get("contents"), channelID, maxResults, videoParser)

		// 디버깅: 구조 변경 감지 시 최상위 contents 키 기록
		var topKeys []string
		data.Get("contents").ForEach(func(key, _ gjson.Result) bool {
			topKeys = append(topKeys, key.String())
			return true
		})

		// tabs 경로가 사라져도 contents 내부에서 videoRenderer를 찾을 수 있으면 복구한다.
		if len(fallbackVideos) > 0 {
			slog.Info("ytInitialData tabs missing but recovered via contents fallback",
				"channel_id", channelID,
				"fallback_videos", len(fallbackVideos),
				"contents_keys", strings.Join(topKeys, ", "))
			return fallbackVideos, nil
		}

		if !hasContents && !hasAlerts {
			slog.Debug("ytInitialData responseContext-only payload",
				"channel_id", channelID,
				"raw_len", len(data.Raw))
			return []*Video{}, nil
		}

		slog.Warn("ytInitialData tabs missing and no recoverable videos",
			"channel_id", channelID,
			"contents_keys", strings.Join(topKeys, ", "),
			"has_contents", hasContents,
			"has_alerts", hasAlerts,
			"raw_len", len(data.Raw))

		// YouTube가 responseContext-only payload를 주거나 구조가 부분적으로 바뀐 경우,
		// 전체 폴러 실패 대신 빈 결과를 반환해 다음 주기에서 재시도한다.
		return []*Video{}, nil
	}

	var videosContent gjson.Result
	var foundTabTitles []string

	tabs.ForEach(func(_, tab gjson.Result) bool {
		tabTitle := tab.Get("tabRenderer.title").String()
		tabURL := tab.Get("tabRenderer.endpoint.commandMetadata.webCommandMetadata.url").String()

		if tabTitle != "" {
			foundTabTitles = append(foundTabTitles, tabTitle)
		}

		isVideosTab := tabTitle == "Videos" || tabTitle == "동영상" || tabTitle == "動画" ||
			strings.Contains(tabURL, "/videos")

		if isVideosTab {
			videosContent = tab.Get("tabRenderer.content")
			return false
		}
		return true
	})

	if !videosContent.Exists() {
		slog.Debug("channel has no videos tab",
			"channel_id", channelID,
			"found_tabs", strings.Join(foundTabTitles, ", "))
		return []*Video{}, nil
	}

	var items []gjson.Result
	richGridItems := videosContent.Get("richGridRenderer.contents")
	if richGridItems.Exists() {
		richGridItems.ForEach(func(_, item gjson.Result) bool {
			if item.Get("richItemRenderer.content.videoRenderer").Exists() {
				items = append(items, item.Get("richItemRenderer.content.videoRenderer"))
			}
			return true
		})
	}

	videos := make([]*Video, 0, min(len(items), maxResults))
	for i, item := range items {
		if i >= maxResults {
			break
		}
		video := videoParser(item, channelID)
		if video != nil {
			videos = append(videos, video)
		}
	}

	return videos, nil
}

// parseVideosFromContentsFallback: tabs 경로가 없을 때 contents 내부 전체에서 videoRenderer를 탐색한다.
func parseVideosFromContentsFallback(
	contents gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(gjson.Result, string) *Video,
) []*Video {
	if !contents.Exists() || maxResults <= 0 {
		return []*Video{}
	}

	videoRenderers := collectVideoRenderers(contents, maxResults)
	videos := make([]*Video, 0, len(videoRenderers))
	for _, renderer := range videoRenderers {
		video := videoParser(renderer, channelID)
		if video != nil {
			videos = append(videos, video)
		}
	}
	return videos
}

// collectVideoRenderers: JSON 트리 전체를 순회하며 videoRenderer를 수집한다.
func collectVideoRenderers(root gjson.Result, maxResults int) []gjson.Result {
	if maxResults <= 0 {
		return nil
	}

	results := make([]gjson.Result, 0, maxResults)
	seen := make(map[string]struct{}, maxResults)

	var walk func(gjson.Result)
	walk = func(node gjson.Result) {
		if len(results) >= maxResults || !node.Exists() {
			return
		}
		if !node.IsArray() && !node.IsObject() {
			return
		}

		node.ForEach(func(key, value gjson.Result) bool {
			if len(results) >= maxResults {
				return false
			}

			if key.String() == "videoRenderer" && value.Exists() {
				videoID := value.Get("videoId").String()
				if videoID != "" {
					if _, ok := seen[videoID]; !ok {
						seen[videoID] = struct{}{}
						results = append(results, value)
					}
				}
				return true
			}

			walk(value)
			return len(results) < maxResults
		})
	}

	walk(root)
	return results
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
