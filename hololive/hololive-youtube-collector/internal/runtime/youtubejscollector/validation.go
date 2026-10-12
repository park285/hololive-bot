package youtubejscollector

import (
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

const maxUnavailableLiveSessions = 32

func validateUnavailableLiveSessions(channelID, kind string, result *youtubejs.ChannelResult) error {
	unavailable := result.UnavailableLiveSessions
	if len(unavailable) == 0 {
		return nil
	}

	if kind != channelKindLive || result.MissingTab || len(unavailable) > maxUnavailableLiveSessions {
		return collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "youtube.js unavailable live sessions are outside the collection scope")
	}

	seen := make(map[string]struct{}, len(result.LiveSessions)+len(unavailable))
	for i := range result.LiveSessions {
		seen[result.LiveSessions[i].VideoID] = struct{}{}
	}

	for _, item := range unavailable {
		if item.ChannelID != channelID || strings.TrimSpace(item.VideoID) == "" || item.Reason != "access_restricted" {
			return collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "youtube.js unavailable live session identity or reason is invalid")
		}

		if _, exists := seen[item.VideoID]; exists {
			return collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "youtube.js unavailable live session overlaps another row")
		}

		seen[item.VideoID] = struct{}{}
	}

	return nil
}

func validateContentIdentity(requestedChannelID string, items []youtubejs.ContentItem) error {
	for i := range items {
		if items[i].ChannelID != requestedChannelID {
			return collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "youtube.js content response identity does not match request")
		}
	}

	return nil
}

func validateLiveIdentity(requestedChannelID string, sessions []youtubejs.LiveSessionItem) error {
	for i := range sessions {
		if sessions[i].ChannelID != requestedChannelID {
			return collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "youtube.js live response identity does not match request")
		}
	}

	return nil
}

func validateLiveSchedules(sessions []youtubejs.LiveSessionItem) error {
	for i := range sessions {
		if sessions[i].Status == "UPCOMING" &&
			(sessions[i].ScheduledAt == nil || sessions[i].ScheduledAt.IsZero()) {
			return collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "youtube.js upcoming live response is missing scheduled time")
		}
	}

	return nil
}

func validateCommunityRows(posts []*youtubejs.CommunityPost) error {
	for _, post := range posts {
		if post == nil {
			return collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "youtube.js community response contains a null row")
		}
	}

	return nil
}

func contentPartialFailureAllowed(class contract.FailureClass) bool {
	switch class {
	case contract.ClassTransient, contract.ClassTimeout, contract.ClassCooldown, contract.ClassResourceLimit:
		return true
	case contract.ClassCanceled, contract.ClassDataContract, contract.ClassConfiguration,
		contract.ClassProtocol, contract.ClassSuperseded, contract.ClassInternal:
		return false
	}

	return false
}
