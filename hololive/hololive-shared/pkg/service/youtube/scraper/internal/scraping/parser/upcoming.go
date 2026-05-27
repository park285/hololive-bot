package parser

import "github.com/tidwall/gjson"

const upcomingSectionsPath = "contents.twoColumnBrowseResultsRenderer.tabs.0.tabRenderer.content.sectionListRenderer.contents"

func ParseUpcomingEventsFromInitialData(data gjson.Result) []*UpcomingEvent {
	events := make([]*UpcomingEvent, 0)
	seen := make(map[string]bool)

	data.Get(upcomingSectionsPath).ForEach(func(_, section gjson.Result) bool {
		contents := section.Get("itemSectionRenderer.contents")

		contents.ForEach(func(_, content gjson.Result) bool {
			appendUpcomingEventsFromFeaturedItems(&events, seen, content.Get("channelFeaturedContentRenderer.items"))
			appendUpcomingEventsFromShelfItems(&events, seen, content.Get("shelfRenderer.content.horizontalListRenderer.items"))
			return true
		})
		return true
	})

	return events
}

func appendUpcomingEventsFromFeaturedItems(events *[]*UpcomingEvent, seen map[string]bool, items gjson.Result) {
	items.ForEach(func(_, item gjson.Result) bool {
		appendUpcomingEvent(events, seen, item.Get("videoRenderer"))
		return true
	})
}

func appendUpcomingEventsFromShelfItems(events *[]*UpcomingEvent, seen map[string]bool, items gjson.Result) {
	items.ForEach(func(_, item gjson.Result) bool {
		video := item.Get("videoRenderer")
		if !video.Exists() {
			video = item.Get("gridVideoRenderer")
		}
		appendUpcomingEvent(events, seen, video)
		return true
	})
}

func appendUpcomingEvent(events *[]*UpcomingEvent, seen map[string]bool, video gjson.Result) {
	if !video.Exists() {
		return
	}

	event := parseVideoToEvent(video)
	if event == nil || seen[event.VideoID] {
		return
	}
	if event.Status != "LIVE" && event.Status != "UPCOMING" {
		return
	}

	seen[event.VideoID] = true
	*events = append(*events, event)
}

func parseVideoToEvent(video gjson.Result) *UpcomingEvent {
	videoID := video.Get("videoId").String()
	if videoID == "" {
		return nil
	}

	return &UpcomingEvent{
		VideoID:       videoID,
		Title:         videoTitleText(video),
		Thumbnail:     ParseThumbnailSources(video.Get("thumbnail.thumbnails")),
		Status:        videoEventStatus(video),
		StartTime:     videoEventStartTime(video),
		ViewCountText: videoViewCountText(video),
		ChannelTitle:  video.Get("shortBylineText.runs.0.text").String(),
	}
}

func videoEventStatus(video gjson.Result) string {
	status := thumbnailOverlayEventStatus(video)
	if status != "DEFAULT" {
		return status
	}
	if video.Get("upcomingEventData").Exists() {
		return "UPCOMING"
	}
	return status
}

func thumbnailOverlayEventStatus(video gjson.Result) string {
	status := "DEFAULT"
	video.Get("thumbnailOverlays").ForEach(func(_, overlay gjson.Result) bool {
		style := overlay.Get("thumbnailOverlayTimeStatusRenderer.style").String()
		if style == "LIVE" || style == "UPCOMING" {
			status = style
			return false
		}
		return true
	})
	return status
}

func videoEventStartTime(video gjson.Result) *int64 {
	st := video.Get("upcomingEventData.startTime").Int()
	if st <= 0 {
		return nil
	}
	return &st
}

func videoTitleText(video gjson.Result) string {
	if title := video.Get("title.simpleText").String(); title != "" {
		return title
	}
	return video.Get("title.runs.0.text").String()
}

func videoViewCountText(video gjson.Result) string {
	if text := video.Get("viewCountText.simpleText").String(); text != "" {
		return text
	}
	return video.Get("viewCountText.runs.0.text").String()
}
