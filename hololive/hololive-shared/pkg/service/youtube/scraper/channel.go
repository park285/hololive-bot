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

	"github.com/tidwall/gjson"
)

func (c *Client) GetChannelStats(ctx context.Context, channelID string) (*ChannelStats, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s/about", channelID)

	html, err := c.fetchPage(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		logStructureWarning("channel_stats", channelID, "ytInitialData extraction failed", "error", err)
		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
	}

	data := gjson.Parse(jsonStr)
	if err := checkAlerts(data); err != nil {
		return nil, err
	}

	stats := parseChannelStatsFromInitialData(data, channelID)
	if looksEmptyChannelStats(stats) {
		logStructureWarning("channel_stats", channelID, "parsed stats are empty")
	}

	return stats, nil
}

func (c *Client) GetChannelSnippet(ctx context.Context, channelID string) (*ChannelSnippet, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)

	html, err := c.fetchPage(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		logStructureWarning("channel_snippet", channelID, "ytInitialData extraction failed", "error", err)
		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
	}

	data := gjson.Parse(jsonStr)
	if err := checkAlerts(data); err != nil {
		return nil, err
	}
	snippet := parseChannelSnippetFromInitialData(data)

	if len(snippet.Avatar) == 0 || len(snippet.Banner) == 0 {
		logStructureWarning("channel_snippet", channelID, "page header images missing",
			"avatar_count", len(snippet.Avatar),
			"banner_count", len(snippet.Banner))
	}

	return snippet, nil
}
