package scraper

import (
	"context"
	"fmt"
	"log/slog"

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

	return parseChannelStatsFromInitialData(data, channelID), nil
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
	snippet := parseChannelSnippetFromInitialData(data)

	if len(snippet.Avatar) == 0 || len(snippet.Banner) == 0 {
		slog.Debug("channel snippet missing page header images",
			"channel_id", channelID,
			"avatar_count", len(snippet.Avatar),
			"banner_count", len(snippet.Banner))
	}

	return snippet, nil
}
