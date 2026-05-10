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
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const aboutChannelViewModelPath = "onResponseReceivedEndpoints.0.showEngagementPanelEndpoint.engagementPanel.engagementPanelSectionListRenderer.content.sectionListRenderer.contents.0.itemSectionRenderer.contents.0.aboutChannelRenderer.metadata.aboutChannelViewModel"

func parseChannelStatsFromInitialData(data gjson.Result, channelID string) *ChannelStats {
	stats := &ChannelStats{
		ChannelID: channelID,
	}

	subscriberText := data.Get(aboutChannelViewModelPath + ".subscriberCountText").String()
	stats.SubscriberCount = parseSubscriberCount(subscriberText)

	viewCountText := data.Get(aboutChannelViewModelPath + ".viewCountText").String()
	stats.ViewCount = parseViewCount(viewCountText)

	videoCountText := data.Get(aboutChannelViewModelPath + ".videoCountText").String()
	stats.VideoCount = parseVideoCount(videoCountText)

	joinedText := data.Get(aboutChannelViewModelPath + ".joinedDateText.content").String()
	stats.JoinedDate = parseJoinedDate(joinedText)

	stats.Description = data.Get(aboutChannelViewModelPath + ".description").String()
	stats.Country = data.Get(aboutChannelViewModelPath + ".country").String()
	stats.Handle = parseChannelHandle(data)

	return stats
}

func parseChannelSnippetFromInitialData(data gjson.Result) *ChannelSnippet {
	return &ChannelSnippet{
		Avatar: parseThumbnailSources(data.Get("header.pageHeaderRenderer.content.pageHeaderViewModel.image.decoratedAvatarViewModel.avatar.avatarViewModel.image.sources")),
		Banner: parseThumbnailSources(data.Get("header.pageHeaderRenderer.content.pageHeaderViewModel.banner.imageBannerViewModel.image.sources")),
	}
}

func parseChannelHandle(data gjson.Result) string {
	handle := data.Get("contents.twoColumnBrowseResultsRenderer.tabs.0.tabRenderer.endpoint.browseEndpoint.canonicalBaseUrl").String()
	if len(handle) > 1 && handle[0] == '/' {
		return handle[1:]
	}
	return handle
}

func parseThumbnailSources(sources gjson.Result) []Thumbnail {
	thumbnails := make([]Thumbnail, 0)
	sources.ForEach(func(_, img gjson.Result) bool {
		thumbnails = append(thumbnails, Thumbnail{
			URL:    img.Get("url").String(),
			Width:  int(img.Get("width").Int()),
			Height: int(img.Get("height").Int()),
		})
		return true
	})
	return thumbnails
}

// parseShortNumber: "2.76M", "1.5K", "500" 등을 정수로 변환
func parseShortNumber(text string) int64 {
	text = strings.TrimSpace(text)
	if text == "" || text == "No" {
		return 0
	}

	multiplier := int64(1)
	switch {
	case strings.HasSuffix(text, "K"):
		multiplier = 1_000
		text = strings.TrimSuffix(text, "K")
	case strings.HasSuffix(text, "M"):
		multiplier = 1_000_000
		text = strings.TrimSuffix(text, "M")
	case strings.HasSuffix(text, "B"):
		multiplier = 1_000_000_000
		text = strings.TrimSuffix(text, "B")
	}

	text = strings.ReplaceAll(text, ",", "")

	val, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}

	return int64(val * float64(multiplier))
}

// parseSubscriberCount: "2.76M subscribers"를 2760000으로 변환
func parseSubscriberCount(text string) int64 {
	text = strings.TrimSuffix(text, " subscribers")
	text = strings.TrimSuffix(text, " subscriber")
	return parseShortNumber(text)
}

// parseViewCount: "1,056,229,686 views"를 정수로 변환
func parseViewCount(text string) int64 {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, " views")
	text = strings.TrimSuffix(text, " view")
	text = strings.TrimSuffix(text, "回視聴")
	text = strings.TrimPrefix(text, "조회수")
	text = strings.TrimSuffix(text, "회")
	text = strings.TrimSpace(text)

	multiplier := float64(1)
	for _, unit := range []struct {
		suffix string
		value  float64
	}{
		{"K", 1_000},
		{"M", 1_000_000},
		{"B", 1_000_000_000},
		{"천", 1_000},
		{"만", 10_000},
		{"万", 10_000},
	} {
		if strings.HasSuffix(text, unit.suffix) {
			text = strings.TrimSuffix(text, unit.suffix)
			multiplier = unit.value
			break
		}
	}

	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimSpace(text)
	val, _ := strconv.ParseFloat(text, 64)
	return int64(val * multiplier)
}

// parseVideoCount: "2,429 videos"를 정수로 변환
func parseVideoCount(text string) int64 {
	text = strings.TrimSuffix(text, " videos")
	text = strings.TrimSuffix(text, " video")
	text = strings.ReplaceAll(text, ",", "")
	val, _ := strconv.ParseInt(text, 10, 64)
	return val
}

// parseJoinedDate: "Joined Jul 2, 2019"를 Unix timestamp로 변환
func parseJoinedDate(text string) int64 {
	text = strings.TrimPrefix(text, "Joined ")
	if text == "" {
		return 0
	}

	formats := []string{
		"Jan 2, 2006",
		"January 2, 2006",
		"2 Jan 2006",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, text); err == nil {
			return t.Unix()
		}
	}

	return 0
}
