package parser

import (
	"log/slog"
	"strings"

	"github.com/tidwall/gjson"
)

const MaxVideoRendererFallbackNodes = 4096

func ParseVideosFromInitialData(
	data *gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(*gjson.Result, string) *Video,
) ([]*Video, error) {
	tabs := data.Get("contents.twoColumnBrowseResultsRenderer.tabs")
	if !tabs.Exists() {
		return ParseVideosFromInitialDataWithoutTabs(data, channelID, maxResults, videoParser), nil
	}

	videosContent, foundTabTitles := FindVideosTabContent(&tabs)
	if !videosContent.Exists() {
		slog.Debug("channel has no videos tab",
			"channel_id", channelID,
			"found_tabs", strings.Join(foundTabTitles, ", "))
		return []*Video{}, nil
	}

	return parseVideosFromRichGrid(&videosContent, channelID, maxResults, videoParser), nil
}

func ParseVideosFromInitialDataWithoutTabs(
	data *gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(*gjson.Result, string) *Video,
) []*Video {
	hasContents := data.Get("contents").Exists()
	hasAlerts := data.Get("alerts").Exists()
	contents := data.Get("contents")
	fallbackVideos := parseVideosFromContentsFallback(&contents, channelID, maxResults, videoParser)
	topKeys := collectTopLevelKeys(&contents)

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

func collectTopLevelKeys(contents *gjson.Result) []string {
	var topKeys []string
	contents.ForEach(func(key, _ gjson.Result) bool {
		topKeys = append(topKeys, key.String())
		return true
	})
	return topKeys
}

var videosTabTitles = map[string]struct{}{
	"Videos":   {},
	"동영상":      {},
	"動画":       {},
	"视频":       {},
	"影片":       {},
	"Vídeos":   {},
	"Видео":    {},
	"Vidéos":   {},
	"Vidéo":    {},
	"Video":    {},
	"วิดีโอ":   {},
	"वीडियो":   {},
	"Filmer":   {},
	"Filmy":    {},
	"Фільми":   {},
	"फ़िल्में": {},
}

func isVideosTabTitle(title string) bool {
	if title == "" {
		return false
	}
	_, ok := videosTabTitles[title]
	return ok
}

func FindVideosTabContent(tabs *gjson.Result) (result1 gjson.Result, result2 []string) {
	var videosContent gjson.Result
	var foundTabTitles []string

	tabs.ForEach(func(_, tab gjson.Result) bool {
		tabTitle := tab.Get("tabRenderer.title").String()
		tabURL := tab.Get("tabRenderer.endpoint.commandMetadata.webCommandMetadata.url").String()

		if tabTitle != "" {
			foundTabTitles = append(foundTabTitles, tabTitle)
		}

		if !isVideosTabMatch(tabTitle, tabURL) {
			return true
		}

		videosContent = tab.Get("tabRenderer.content")
		return false
	})

	return videosContent, foundTabTitles
}

func isVideosTabMatch(tabTitle, tabURL string) bool {
	if isVideosTabTitle(tabTitle) {
		return true
	}
	return strings.Contains(tabURL, "/videos")
}

func parseVideosFromRichGrid(
	videosContent *gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(*gjson.Result, string) *Video,
) []*Video {
	richGridItems := videosContent.Get("richGridRenderer.contents")
	items := collectRecentVideoItems(&richGridItems)
	videos := make([]*Video, 0, min(len(items), maxResults))
	for i, item := range items {
		if i >= maxResults {
			break
		}
		if video := parseRecentVideoItem(&item, channelID, videoParser); video != nil {
			videos = append(videos, video)
		}
	}
	return videos
}

func parseRecentVideoItem(item *gjson.Result, channelID string, videoParser func(*gjson.Result, string) *Video) *Video {
	if renderer := item.Get("videoRenderer"); renderer.Exists() {
		return videoParser(&renderer, channelID)
	}
	if lockup := item.Get("lockupViewModel"); lockup.Exists() {
		return ParseLockupVideoViewModel(&lockup, channelID)
	}
	return nil
}

func collectRecentVideoItems(richGridItems *gjson.Result) []gjson.Result {
	var items []gjson.Result
	if !richGridItems.Exists() {
		return items
	}
	richGridItems.ForEach(func(_, item gjson.Result) bool {
		videoRenderer := item.Get("richItemRenderer.content.videoRenderer")
		if videoRenderer.Exists() {
			items = append(items, gjson.Parse(`{"videoRenderer":`+videoRenderer.Raw+`}`))
			return true
		}

		lockupViewModel := item.Get("richItemRenderer.content.lockupViewModel")
		if lockupViewModel.Get("contentType").String() == "LOCKUP_CONTENT_TYPE_VIDEO" {
			items = append(items, gjson.Parse(`{"lockupViewModel":`+lockupViewModel.Raw+`}`))
		}
		return true
	})
	return items
}

func parseVideosFromContentsFallback(
	contents *gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(*gjson.Result, string) *Video,
) []*Video {
	if !contents.Exists() || maxResults <= 0 {
		return []*Video{}
	}

	videoRenderers := CollectVideoRenderers(contents, maxResults)
	videos := make([]*Video, 0, len(videoRenderers))
	for _, renderer := range videoRenderers {
		video := videoParser(&renderer, channelID)
		if video != nil {
			videos = append(videos, video)
		}
	}
	return videos
}

func CollectVideoRenderers(root *gjson.Result, maxResults int) []gjson.Result {
	if maxResults <= 0 {
		return nil
	}

	collector := newVideoRendererCollector(maxResults)
	collector.walk(root)
	return collector.results
}

type videoRendererCollector struct {
	results    []gjson.Result
	seen       map[string]struct{}
	visited    int
	maxResults int
}

func newVideoRendererCollector(maxResults int) *videoRendererCollector {
	return &videoRendererCollector{
		results:    make([]gjson.Result, 0, maxResults),
		seen:       make(map[string]struct{}, maxResults),
		maxResults: maxResults,
	}
}

func (c *videoRendererCollector) walk(node *gjson.Result) {
	if !c.canVisit(node) {
		return
	}

	c.visited++
	node.ForEach(func(key, value gjson.Result) bool {
		return c.visit(&key, &value)
	})
}

func (c *videoRendererCollector) canVisit(node *gjson.Result) bool {
	if c.shouldStop() || !node.Exists() {
		return false
	}
	return node.IsArray() || node.IsObject()
}

func (c *videoRendererCollector) shouldStop() bool {
	return len(c.results) >= c.maxResults || c.visited >= MaxVideoRendererFallbackNodes
}

func (c *videoRendererCollector) visit(key, value *gjson.Result) bool {
	if c.shouldStop() {
		return false
	}
	if key.String() == "videoRenderer" {
		c.add(value)
		return true
	}

	c.walk(value)
	return !c.shouldStop()
}

func (c *videoRendererCollector) add(value *gjson.Result) {
	videoID := value.Get("videoId").String()
	if videoID == "" {
		return
	}
	if _, ok := c.seen[videoID]; ok {
		return
	}

	c.seen[videoID] = struct{}{}
	c.results = append(c.results, *value)
}

func ParseLockupVideoViewModel(lockup *gjson.Result, channelID string) *Video {
	if lockup.Get("contentType").String() != "LOCKUP_CONTENT_TYPE_VIDEO" {
		return nil
	}

	videoID := lockup.Get("contentId").String()
	if videoID == "" {
		videoID = lockup.Get("rendererContext.commandContext.onTap.innertubeCommand.watchEndpoint.videoId").String()
	}
	if videoID == "" {
		return nil
	}

	var thumbnails []Thumbnail
	lockup.Get("contentImage.thumbnailViewModel.image.sources").ForEach(func(_, t gjson.Result) bool {
		thumbnails = append(thumbnails, Thumbnail{
			URL:    t.Get("url").String(),
			Width:  int(t.Get("width").Int()),
			Height: int(t.Get("height").Int()),
		})
		return true
	})

	metadataParts := lockup.Get("metadata.lockupMetadataViewModel.metadata.contentMetadataViewModel.metadataRows.0.metadataParts")
	viewCount, publishedText := PickLockupMetadataTexts(&metadataParts)

	return &Video{
		VideoID:       videoID,
		Title:         lockup.Get("metadata.lockupMetadataViewModel.title.content").String(),
		Thumbnail:     thumbnails,
		ViewCount:     viewCount,
		PublishedText: publishedText,
		Duration:      lockup.Get("contentImage.thumbnailViewModel.overlays.0.thumbnailBottomOverlayViewModel.badges.0.thumbnailBadgeViewModel.text").String(),
		ChannelID:     channelID,
		Source:        VideoSourceHTML,
	}
}

func PickLockupMetadataTexts(parts *gjson.Result) (viewCount int64, publishedText string) {
	texts := CollectLockupTexts(parts)
	if viewCount, published, ok := PickViewCountAndPublished(texts); ok {
		return viewCount, published
	}
	return FallbackPickMetadata(texts)
}

func CollectLockupTexts(parts *gjson.Result) []string {
	var texts []string
	parts.ForEach(func(_, part gjson.Result) bool {
		text := part.Get("text.content").String()
		if text != "" {
			texts = append(texts, text)
		}
		return true
	})
	return texts
}

func PickViewCountAndPublished(texts []string) (viewCount int64, publishedText string, ok bool) {
	for i, t := range texts {
		parsed := ParseViewCount(t)
		if parsed <= 0 {
			continue
		}
		return parsed, firstOtherText(texts, i), true
	}
	return 0, "", false
}

func firstOtherText(texts []string, excludeIdx int) string {
	for i, t := range texts {
		if i == excludeIdx {
			continue
		}
		return t
	}
	return ""
}

func FallbackPickMetadata(texts []string) (result1 int64, result2 string) {
	var viewText, publishedText string
	if len(texts) > 0 {
		viewText = texts[0]
	}
	if len(texts) > 1 {
		publishedText = texts[1]
	}
	return ParseViewCount(viewText), publishedText
}
