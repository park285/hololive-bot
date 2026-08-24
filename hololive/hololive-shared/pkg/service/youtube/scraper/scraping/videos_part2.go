package scraping

import (
	"github.com/tidwall/gjson"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

func findPopularGridVideoRenderers(data *gjson.Result) []gjson.Result {
	var popularItems []gjson.Result

	popularSections(data).ForEach(func(_, section gjson.Result) bool {
		if !isPopularVideosShelf(&section) {
			return true
		}

		popularItems = collectGridVideoRenderers(&section)

		return false
	})

	return popularItems
}

func popularSections(data *gjson.Result) gjson.Result {
	sectionsPath := "contents.twoColumnBrowseResultsRenderer.tabs.0.tabRenderer.content.sectionListRenderer.contents"
	return data.Get(sectionsPath)
}

func isPopularVideosShelf(section *gjson.Result) bool {
	shelfTitle := section.Get("itemSectionRenderer.contents.0.shelfRenderer.title.runs.0.text").String()
	return shelfTitle == "Popular videos" || shelfTitle == "Popular"
}

func collectGridVideoRenderers(section *gjson.Result) []gjson.Result {
	var gridVideos []gjson.Result

	gridItems := section.Get("itemSectionRenderer.contents.0.shelfRenderer.content.gridRenderer.items")
	gridItems.ForEach(func(_, item gjson.Result) bool {
		if item.Get("gridVideoRenderer").Exists() {
			gridVideos = append(gridVideos, item.Get("gridVideoRenderer"))
		}

		return true
	})

	return gridVideos
}

func (c *Client) parsePopularGridVideos(popularItems []gjson.Result, channelID string, maxResults int) []*parser.Video {
	videos := make([]*parser.Video, 0, min(len(popularItems), maxResults))
	for i, item := range popularItems {
		if i >= maxResults {
			break
		}

		video := c.parseGridVideoRenderer(&item, channelID)
		if video != nil {
			videos = append(videos, video)
		}
	}

	return videos
}

func (c *Client) parseVideoCommon(video *gjson.Result, channelID, durationPath, channelTitlePath, channelHandlePath string) *parser.Video {
	videoID := video.Get("videoId").String()
	if videoID == "" {
		return nil
	}

	var thumbnails []parser.Thumbnail

	video.Get("thumbnail.thumbnails").ForEach(func(_, t gjson.Result) bool {
		thumbnails = append(thumbnails, parser.Thumbnail{
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

	return &parser.Video{
		VideoID:       videoID,
		Title:         title,
		Thumbnail:     thumbnails,
		ViewCount:     viewCount,
		PublishedText: video.Get("publishedTimeText.simpleText").String(),
		Duration:      video.Get(durationPath).String(),
		ChannelID:     channelID,
		ChannelTitle:  video.Get(channelTitlePath).String(),
		ChannelHandle: video.Get(channelHandlePath).String(),
		Source:        parser.VideoSourceHTML,
	}
}

func (c *Client) parseVideoRenderer(video *gjson.Result, channelID string) *parser.Video {
	return c.parseVideoCommon(video, channelID,
		"lengthText.simpleText",
		"ownerText.runs.0.text",
		"ownerText.runs.0.navigationEndpoint.browseEndpoint.canonicalBaseUrl",
	)
}

func (c *Client) parseGridVideoRenderer(video *gjson.Result, channelID string) *parser.Video {
	return c.parseVideoCommon(video, channelID,
		"thumbnailOverlays.0.thumbnailOverlayTimeStatusRenderer.text.simpleText",
		"shortBylineText.runs.0.text",
		"shortBylineText.runs.0.navigationEndpoint.browseEndpoint.canonicalBaseUrl",
	)
}
