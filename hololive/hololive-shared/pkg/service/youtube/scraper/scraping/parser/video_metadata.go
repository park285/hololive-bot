package parser

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var ErrReplayStatusNotFound = errors.New("video replay status not found")

var (
	replayLiveContentPattern    = regexp.MustCompile(`(?i)"isLiveContent"\s*:\s*true`)
	replayNotLiveContentPattern = regexp.MustCompile(`(?i)"isLiveContent"\s*:\s*false`)
	replayBroadcastPattern      = regexp.MustCompile(`(?i)"liveBroadcastDetails"\s*:`)
)

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
