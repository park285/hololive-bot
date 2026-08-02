package parser

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/tidwall/gjson"

	initialdata "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/initialdata"
)

var ErrReplayStatusNotFound = errors.New("video replay status not found")

var (
	replayLiveContentPattern    = regexp.MustCompile(`(?i)"isLiveContent"\s*:\s*true`)
	replayNotLiveContentPattern = regexp.MustCompile(`(?i)"isLiveContent"\s*:\s*false`)
	replayBroadcastPattern      = regexp.MustCompile(`(?i)"liveBroadcastDetails"\s*:`)
)

const maxInitialPlayerResponseCandidates = 4

func ExtractWatchLiveMetadata(html string) WatchLiveMetadata {
	playerResponse, ok := extractInitialPlayerResponse(html)
	if !ok {
		return WatchLiveMetadata{LiveContent: LiveContentUnknown}
	}

	metadata := WatchLiveMetadata{LiveContent: LiveContentUnknown}
	liveContent := playerResponse.Get("videoDetails.isLiveContent")
	if liveContent.Exists() && liveContent.Type == gjson.True {
		metadata.LiveContent = LiveContentTrue
	} else if liveContent.Exists() && liveContent.Type == gjson.False {
		metadata.LiveContent = LiveContentFalse
	}

	startTimestamp := playerResponse.Get("microformat.playerMicroformatRenderer.liveBroadcastDetails.startTimestamp").String()
	if timestamp, err := time.Parse(time.RFC3339, startTimestamp); err == nil {
		metadata.StartTimestamp = &timestamp
	}

	return metadata
}

func extractInitialPlayerResponse(html string) (gjson.Result, bool) {
	for _, candidate := range initialdata.ExtractPlayerResponseCandidates(html, maxInitialPlayerResponseCandidates) {
		playerResponse := gjson.Parse(candidate)
		if playerResponse.IsObject() {
			return playerResponse, true
		}
	}
	return gjson.Result{}, false
}

func ExtractVideoMetadataFromHTML(html string) VideoMetadata {
	return VideoMetadata{
		PublishedAt: extractOptionalPublishedAt(html),
		Replay:      DetectReplayStatus(html),
	}
}

func extractOptionalPublishedAt(html string) *time.Time {
	publishedAt, err := ExtractPublishedAtFromHTML(html)
	if err != nil {
		return nil
	}

	return publishedAt
}

func DetectReplayStatus(html string) ReplayStatus {
	if replayBroadcastPattern.MatchString(html) || replayLiveContentPattern.MatchString(html) || hasLiveBroadcastMeta(html) {
		return ReplayStatusReplay
	}
	if replayNotLiveContentPattern.MatchString(html) {
		return ReplayStatusNotReplay
	}

	return ReplayStatusUnknown
}

func hasLiveBroadcastMeta(html string) bool {
	document, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return false
	}
	found := false
	document.Find("meta").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		if !strings.EqualFold(strings.TrimSpace(selection.AttrOr("itemprop", "")), "isLiveBroadcast") {
			return true
		}
		found = strings.EqualFold(strings.TrimSpace(selection.AttrOr("content", "")), "true")
		return !found
	})
	return found
}
