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
	"log/slog"
	"strings"

	"github.com/tidwall/gjson"
)

const maxVideoRendererFallbackNodes = 4096

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

	tabs := data.Get("contents.twoColumnBrowseResultsRenderer.tabs")
	if !tabs.Exists() {
		return parseVideosFromInitialDataWithoutTabs(data, channelID, maxResults, videoParser), nil
	}

	videosContent, foundTabTitles := findVideosTabContent(tabs)
	if !videosContent.Exists() {
		slog.Debug("channel has no videos tab",
			"channel_id", channelID,
			"found_tabs", strings.Join(foundTabTitles, ", "))
		return []*Video{}, nil
	}

	return parseVideosFromRichGrid(videosContent, channelID, maxResults, videoParser), nil
}

func parseVideosFromInitialDataWithoutTabs(
	data gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(gjson.Result, string) *Video,
) []*Video {
	hasContents := data.Get("contents").Exists()
	hasAlerts := data.Get("alerts").Exists()
	fallbackVideos := parseVideosFromContentsFallback(data.Get("contents"), channelID, maxResults, videoParser)
	topKeys := collectTopLevelKeys(data.Get("contents"))

	if len(fallbackVideos) > 0 {
		slog.Info("ytInitialData tabs missing but recovered via contents fallback",
			"channel_id", channelID,
			"fallback_videos", len(fallbackVideos),
			"contents_keys", strings.Join(topKeys, ", "))
		return fallbackVideos
	}

	if !hasContents && !hasAlerts {
		slog.Debug("ytInitialData responseContext-only payload",
			"channel_id", channelID,
			"raw_len", len(data.Raw))
		return []*Video{}
	}

	slog.Warn("ytInitialData tabs missing and no recoverable videos",
		"channel_id", channelID,
		"contents_keys", strings.Join(topKeys, ", "),
		"has_contents", hasContents,
		"has_alerts", hasAlerts,
		"raw_len", len(data.Raw))

	return []*Video{}
}

func collectTopLevelKeys(contents gjson.Result) []string {
	var topKeys []string
	contents.ForEach(func(key, _ gjson.Result) bool {
		topKeys = append(topKeys, key.String())
		return true
	})
	return topKeys
}

func findVideosTabContent(tabs gjson.Result) (gjson.Result, []string) {
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
		if !isVideosTab {
			return true
		}

		videosContent = tab.Get("tabRenderer.content")
		return false
	})

	return videosContent, foundTabTitles
}

func parseVideosFromRichGrid(
	videosContent gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(gjson.Result, string) *Video,
) []*Video {
	items := collectVideoRendererItems(videosContent.Get("richGridRenderer.contents"))
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
	return videos
}

func collectVideoRendererItems(richGridItems gjson.Result) []gjson.Result {
	var items []gjson.Result
	if !richGridItems.Exists() {
		return items
	}
	richGridItems.ForEach(func(_, item gjson.Result) bool {
		videoRenderer := item.Get("richItemRenderer.content.videoRenderer")
		if videoRenderer.Exists() {
			items = append(items, videoRenderer)
		}
		return true
	})
	return items
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
	visited := 0

	var walk func(gjson.Result)
	walk = func(node gjson.Result) {
		if len(results) >= maxResults || !node.Exists() || visited >= maxVideoRendererFallbackNodes {
			return
		}
		if !node.IsArray() && !node.IsObject() {
			return
		}
		visited++

		node.ForEach(func(key, value gjson.Result) bool {
			if len(results) >= maxResults || visited >= maxVideoRendererFallbackNodes {
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
			return len(results) < maxResults && visited < maxVideoRendererFallbackNodes
		})
	}

	walk(root)
	return results
}
