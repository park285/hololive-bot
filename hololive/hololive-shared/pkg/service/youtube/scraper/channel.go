package scraper

import (
	"context"
	"fmt"

	"github.com/tidwall/gjson"
)

// GetChannelStats: 채널 통계 정보 조회 (구독자 수, 조회수, 비디오 수 등)
func (c *Client) GetChannelStats(ctx context.Context, channelID string) (*ChannelStats, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s/about", channelID)

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

	// aboutChannelViewModel 경로 (YouTube 2025 구조)
	basePath := "onResponseReceivedEndpoints.0.showEngagementPanelEndpoint.engagementPanel.engagementPanelSectionListRenderer.content.sectionListRenderer.contents.0.itemSectionRenderer.contents.0.aboutChannelRenderer.metadata.aboutChannelViewModel"

	stats := &ChannelStats{
		ChannelID: channelID,
	}

	// 구독자 수 파싱 (예: "2.76M subscribers")
	subscriberText := data.Get(basePath + ".subscriberCountText").String()
	stats.SubscriberCount = parseSubscriberCount(subscriberText)

	// 조회수 파싱
	viewCountText := data.Get(basePath + ".viewCountText").String()
	stats.ViewCount = parseViewCount(viewCountText)

	// 비디오 수 파싱
	videoCountText := data.Get(basePath + ".videoCountText").String()
	stats.VideoCount = parseVideoCount(videoCountText)

	// 가입일 파싱 (예: "Joined Jul 2, 2019")
	joinedText := data.Get(basePath + ".joinedDateText.content").String()
	stats.JoinedDate = parseJoinedDate(joinedText)

	// 설명
	stats.Description = data.Get(basePath + ".description").String()

	// 국가
	stats.Country = data.Get(basePath + ".country").String()

	// Handle 추출 (tabs에서)
	handlePath := "contents.twoColumnBrowseResultsRenderer.tabs.0.tabRenderer.endpoint.browseEndpoint.canonicalBaseUrl"
	handle := data.Get(handlePath).String()
	if len(handle) > 1 && handle[0] == '/' {
		stats.Handle = handle[1:] // 앞의 "/" 제거
	}

	return stats, nil
}

// GetChannelSnippet: 채널 프로필 정보 조회 (아바타, 배너)
func (c *Client) GetChannelSnippet(ctx context.Context, channelID string) (*ChannelSnippet, error) {
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

	snippet := &ChannelSnippet{}

	// YouTube 2025: pageHeaderRenderer 구조에서 avatar/banner 추출
	avatarPath := "header.pageHeaderRenderer.content.pageHeaderViewModel.image.decoratedAvatarViewModel.avatar.avatarViewModel.image.sources"
	bannerPath := "header.pageHeaderRenderer.content.pageHeaderViewModel.banner.imageBannerViewModel.image.sources"

	// Avatar 파싱
	data.Get(avatarPath).ForEach(func(_, img gjson.Result) bool {
		snippet.Avatar = append(snippet.Avatar, Thumbnail{
			URL:    img.Get("url").String(),
			Width:  int(img.Get("width").Int()),
			Height: int(img.Get("height").Int()),
		})
		return true
	})

	// Banner 파싱
	data.Get(bannerPath).ForEach(func(_, img gjson.Result) bool {
		snippet.Banner = append(snippet.Banner, Thumbnail{
			URL:    img.Get("url").String(),
			Width:  int(img.Get("width").Int()),
			Height: int(img.Get("height").Int()),
		})
		return true
	})

	// 기존 c4TabbedHeaderRenderer 폴백 (YouTube 구버전)
	if len(snippet.Avatar) == 0 {
		fallbackAvatarPath := "header.c4TabbedHeaderRenderer.avatar.thumbnails"
		data.Get(fallbackAvatarPath).ForEach(func(_, img gjson.Result) bool {
			snippet.Avatar = append(snippet.Avatar, Thumbnail{
				URL:    img.Get("url").String(),
				Width:  int(img.Get("width").Int()),
				Height: int(img.Get("height").Int()),
			})
			return true
		})
	}

	if len(snippet.Banner) == 0 {
		fallbackBannerPath := "header.c4TabbedHeaderRenderer.banner.thumbnails"
		data.Get(fallbackBannerPath).ForEach(func(_, img gjson.Result) bool {
			snippet.Banner = append(snippet.Banner, Thumbnail{
				URL:    img.Get("url").String(),
				Width:  int(img.Get("width").Int()),
				Height: int(img.Get("height").Int()),
			})
			return true
		})
	}

	return snippet, nil
}
