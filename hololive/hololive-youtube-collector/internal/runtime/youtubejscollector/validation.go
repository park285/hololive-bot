package youtubejscollector

import (
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

const maxUnavailableLiveSessions = 32

func validateUnavailableLiveSessions(channelID, kind string, result *youtubejs.ChannelResult) error {
	unavailable := result.UnavailableLiveSessions
	if len(unavailable) == 0 {
		return nil
	}

	if kind != "live" || result.MissingTab || len(unavailable) > maxUnavailableLiveSessions {
		return collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "youtube.js unavailable live sessions are outside the collection scope")
	}

	seen := make(map[string]struct{}, len(result.LiveSessions)+len(unavailable))
	for i := range result.LiveSessions {
		seen[result.LiveSessions[i].VideoID] = struct{}{}
	}

	for _, item := range unavailable {
		if item.ChannelID != channelID || strings.TrimSpace(item.VideoID) == "" || item.Reason != "access_restricted" {
			return collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "youtube.js unavailable live session identity or reason is invalid")
		}

		if _, exists := seen[item.VideoID]; exists {
			return collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "youtube.js unavailable live session overlaps another row")
		}

		seen[item.VideoID] = struct{}{}
	}

	return nil
}

func validateViewerIdentity(requestedVideoID string, result *youtubejs.ViewerResult) error {
	if result == nil || result.VideoID != requestedVideoID {
		return collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "youtube.js viewer response identity does not match request")
	}

	return nil
}

func validateContentIdentity(requestedChannelID string, items []youtubejs.ContentItem) error {
	for i := range items {
		if items[i].ChannelID != requestedChannelID {
			return collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "youtube.js content response identity does not match request")
		}
	}

	return nil
}

func validateLiveIdentity(requestedChannelID string, sessions []youtubejs.LiveSessionItem) error {
	for i := range sessions {
		if sessions[i].ChannelID != requestedChannelID {
			return collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "youtube.js live response identity does not match request")
		}
	}

	return nil
}

func validateLiveSchedules(sessions []youtubejs.LiveSessionItem) error {
	for i := range sessions {
		if sessions[i].Status == "UPCOMING" &&
			(sessions[i].ScheduledAt == nil || sessions[i].ScheduledAt.IsZero()) {
			return collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "youtube.js upcoming live response is missing scheduled time")
		}
	}

	return nil
}

func validateCommunityRows(posts []*parser.CommunityPost) error {
	for _, post := range posts {
		if post == nil {
			return collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "youtube.js community response contains a null row")
		}
	}

	return nil
}

func contentPartialFailureAllowed(class contract.FailureClass) bool {
	switch class {
	case collecterr.ClassTransient, collecterr.ClassTimeout, collecterr.ClassCooldown, collecterr.ClassResourceLimit:
		return true
	case collecterr.ClassCanceled, collecterr.ClassDataContract, collecterr.ClassConfiguration,
		collecterr.ClassProtocol, collecterr.ClassSuperseded, collecterr.ClassInternal:
		return false
	}

	return false
}
