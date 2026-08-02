package parser

import (
	"strings"

	"github.com/tidwall/gjson"
)

const upcomingSectionsPath = "contents.twoColumnBrowseResultsRenderer.tabs.0.tabRenderer.content.sectionListRenderer.contents"

func ParseUpcomingEventsFromInitialData(data *gjson.Result) []*UpcomingEvent {
	events := make([]*UpcomingEvent, 0)
	seen := make(map[string]bool)

	data.Get(upcomingSectionsPath).ForEach(func(_, section gjson.Result) bool {
		contents := section.Get("itemSectionRenderer.contents")

		contents.ForEach(func(_, content gjson.Result) bool {
			featuredItems := content.Get("channelFeaturedContentRenderer.items")
			appendUpcomingEventsFromFeaturedItems(&events, seen, &featuredItems)
			shelfItems := content.Get("shelfRenderer.content.horizontalListRenderer.items")
			appendUpcomingEventsFromShelfItems(&events, seen, &shelfItems)
			return true
		})
		return true
	})

	return events
}

func appendUpcomingEventsFromFeaturedItems(events *[]*UpcomingEvent, seen map[string]bool, items *gjson.Result) {
	items.ForEach(func(_, item gjson.Result) bool {
		video := item.Get("videoRenderer")
		if video.Exists() {
			appendUpcomingEvent(events, seen, &video)
		} else {
			lockup := item.Get("lockupViewModel")
			appendUpcomingEventFromLockup(events, seen, &lockup)
		}
		return true
	})
}

func appendUpcomingEventsFromShelfItems(events *[]*UpcomingEvent, seen map[string]bool, items *gjson.Result) {
	items.ForEach(func(_, item gjson.Result) bool {
		video := item.Get("videoRenderer")
		if !video.Exists() {
			video = item.Get("gridVideoRenderer")
		}
		if video.Exists() {
			appendUpcomingEvent(events, seen, &video)
		} else {
			lockup := item.Get("lockupViewModel")
			appendUpcomingEventFromLockup(events, seen, &lockup)
		}
		return true
	})
}

func appendUpcomingEvent(events *[]*UpcomingEvent, seen map[string]bool, video *gjson.Result) {
	if !video.Exists() {
		return
	}

	appendParsedUpcomingEvent(events, seen, parseVideoToEvent(video))
}

func appendUpcomingEventFromLockup(events *[]*UpcomingEvent, seen map[string]bool, lockup *gjson.Result) {
	if !lockup.Exists() {
		return
	}

	video := ParseLockupVideoViewModel(lockup, "")
	if video == nil {
		return
	}

	texts := lockupMetadataTexts(lockup)
	status := lockupEventStatus(lockup)
	viewCountText := firstLockupTextWithSuffix(texts, " watching", " waiting")
	appendParsedUpcomingEvent(events, seen, &UpcomingEvent{
		VideoID:       video.VideoID,
		Title:         video.Title,
		Thumbnail:     video.Thumbnail,
		Status:        status,
		ViewCountText: viewCountText,
		ChannelTitle:  firstNonNumericLockupText(texts, viewCountText),
	})
}

func appendParsedUpcomingEvent(events *[]*UpcomingEvent, seen map[string]bool, event *UpcomingEvent) {
	if event == nil || seen[event.VideoID] {
		return
	}
	if event.Status != "LIVE" && event.Status != "UPCOMING" {
		return
	}

	seen[event.VideoID] = true
	*events = append(*events, event)
}

func lockupEventStatus(lockup *gjson.Result) string {
	status := "DEFAULT"
	badges := lockup.Get("contentImage.thumbnailViewModel.overlays.#.thumbnailBottomOverlayViewModel.badges.0.thumbnailBadgeViewModel")
	badges.ForEach(func(_, badge gjson.Result) bool {
		if badge.Get("badgeStyle").String() == "THUMBNAIL_OVERLAY_BADGE_STYLE_LIVE" {
			status = "LIVE"
			return false
		}
		if badge.Get("text").String() == "Upcoming" {
			status = "UPCOMING"
			return false
		}
		return true
	})
	return status
}

func lockupMetadataTexts(lockup *gjson.Result) []string {
	rows := lockup.Get("metadata.lockupMetadataViewModel.metadata.contentMetadataViewModel.metadataRows")
	var texts []string
	rows.ForEach(func(_, row gjson.Result) bool {
		parts := row.Get("metadataParts")
		texts = append(texts, CollectLockupTexts(&parts)...)
		return true
	})
	return texts
}

func firstLockupTextWithSuffix(texts []string, suffixes ...string) string {
	for _, text := range texts {
		for _, suffix := range suffixes {
			if strings.HasSuffix(strings.ToLower(text), suffix) {
				return text
			}
		}
	}
	return ""
}

func firstNonNumericLockupText(texts []string, excluded ...string) string {
	for _, text := range texts {
		if !containsText(excluded, text) && ParseViewCount(text) == 0 {
			return text
		}
	}
	return ""
}

func containsText(texts []string, candidate string) bool {
	for _, text := range texts {
		if text != "" && text == candidate {
			return true
		}
	}
	return false
}

func parseVideoToEvent(video *gjson.Result) *UpcomingEvent {
	videoID := video.Get("videoId").String()
	if videoID == "" {
		return nil
	}

	thumbnails := video.Get("thumbnail.thumbnails")
	return &UpcomingEvent{
		VideoID:       videoID,
		Title:         videoTitleText(video),
		Thumbnail:     ParseThumbnailSources(&thumbnails),
		Status:        videoEventStatus(video),
		StartTime:     videoEventStartTime(video),
		ViewCountText: videoViewCountText(video),
		ChannelTitle:  video.Get("shortBylineText.runs.0.text").String(),
	}
}

func videoEventStatus(video *gjson.Result) string {
	status := thumbnailOverlayEventStatus(video)
	if status != "DEFAULT" {
		return status
	}
	if video.Get("upcomingEventData").Exists() {
		return "UPCOMING"
	}
	return status
}

func thumbnailOverlayEventStatus(video *gjson.Result) string {
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

func videoEventStartTime(video *gjson.Result) *int64 {
	st := video.Get("upcomingEventData.startTime").Int()
	if st <= 0 {
		return nil
	}
	return &st
}

func videoTitleText(video *gjson.Result) string {
	if title := video.Get("title.simpleText").String(); title != "" {
		return title
	}
	return video.Get("title.runs.0.text").String()
}

func videoViewCountText(video *gjson.Result) string {
	if text := video.Get("viewCountText.simpleText").String(); text != "" {
		return text
	}
	return video.Get("viewCountText.runs.0.text").String()
}
