package parser

import (
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const aboutChannelViewModelPath = "onResponseReceivedEndpoints.0.showEngagementPanelEndpoint.engagementPanel.engagementPanelSectionListRenderer.content.sectionListRenderer.contents.0.itemSectionRenderer.contents.0.aboutChannelRenderer.metadata.aboutChannelViewModel"

func ParseChannelStatsFromInitialData(data gjson.Result, channelID string) *ChannelStats {
	stats := &ChannelStats{
		ChannelID: channelID,
	}

	subscriberText := data.Get(aboutChannelViewModelPath + ".subscriberCountText").String()
	stats.SubscriberCount = ParseSubscriberCount(subscriberText)

	viewCountText := data.Get(aboutChannelViewModelPath + ".viewCountText").String()
	stats.ViewCount = ParseViewCount(viewCountText)

	videoCountText := data.Get(aboutChannelViewModelPath + ".videoCountText").String()
	stats.VideoCount = ParseVideoCount(videoCountText)

	joinedText := data.Get(aboutChannelViewModelPath + ".joinedDateText.content").String()
	stats.JoinedDate = ParseJoinedDate(joinedText)

	stats.Description = data.Get(aboutChannelViewModelPath + ".description").String()
	stats.Country = data.Get(aboutChannelViewModelPath + ".country").String()
	stats.Handle = ParseChannelHandle(data)

	return stats
}

func ParseChannelSnippetFromInitialData(data gjson.Result) *ChannelSnippet {
	return &ChannelSnippet{
		Avatar: ParseThumbnailSources(data.Get("header.pageHeaderRenderer.content.pageHeaderViewModel.image.decoratedAvatarViewModel.avatar.avatarViewModel.image.sources")),
		Banner: ParseThumbnailSources(data.Get("header.pageHeaderRenderer.content.pageHeaderViewModel.banner.imageBannerViewModel.image.sources")),
	}
}

func ParseChannelHandle(data gjson.Result) string {
	handle := data.Get("contents.twoColumnBrowseResultsRenderer.tabs.0.tabRenderer.endpoint.browseEndpoint.canonicalBaseUrl").String()
	if len(handle) > 0 && handle[0] == '/' {
		return handle[1:]
	}
	return handle
}

func ParseThumbnailSources(sources gjson.Result) []Thumbnail {
	thumbnails := make([]Thumbnail, 0)
	if !sources.IsArray() {
		return thumbnails
	}
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

func ParseShortNumber(text string) int64 {
	text = strings.TrimSpace(text)
	if text == "" || text == "No" {
		return 0
	}

	text, multiplier := shortNumberBaseAndMultiplier(text)
	text = strings.ReplaceAll(text, ",", "")

	val, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}

	return int64(val * float64(multiplier))
}

func shortNumberBaseAndMultiplier(text string) (string, int64) {
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{"K", 1_000},
		{"M", 1_000_000},
		{"B", 1_000_000_000},
	}
	for _, unit := range units {
		if before, ok := strings.CutSuffix(text, unit.suffix); ok {
			return before, unit.multiplier
		}
	}
	return text, 1
}

func ParseSubscriberCount(text string) int64 {
	text = strings.TrimSuffix(text, " subscribers")
	text = strings.TrimSuffix(text, " subscriber")
	return ParseShortNumber(text)
}

func ParseViewCount(text string) int64 {
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
		{"억", 100_000_000},
	} {
		if before, ok := strings.CutSuffix(text, unit.suffix); ok {
			text = before
			multiplier = unit.value
			break
		}
	}

	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimSpace(text)
	val, _ := strconv.ParseFloat(text, 64)
	return int64(val * multiplier)
}

func ParseVideoCount(text string) int64 {
	text = strings.TrimSuffix(text, " videos")
	text = strings.TrimSuffix(text, " video")
	text = strings.ReplaceAll(text, ",", "")
	val, _ := strconv.ParseInt(text, 10, 64)
	return val
}

func ParseJoinedDate(text string) int64 {
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
