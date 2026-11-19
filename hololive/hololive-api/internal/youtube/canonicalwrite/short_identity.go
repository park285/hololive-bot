package canonicalwrite

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func canonicalizeShortContentIDs(
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
) {
	for i := range notifications {
		if notifications[i] == nil || notifications[i].Kind != domain.OutboxKindNewShort {
			continue
		}

		notifications[i].ContentID = canonicalShortContentID(notifications[i].ContentID)
	}

	for i := range trackingRows {
		if trackingRows[i] == nil || trackingRows[i].Kind != domain.OutboxKindNewShort {
			continue
		}

		trackingRows[i].ContentID = canonicalShortContentID(trackingRows[i].ContentID)
	}
}

func canonicalShortContentID(contentID string) string {
	if canonicalID := normalizeContentID(domain.OutboxKindNewShort, contentID); canonicalID != "" {
		return canonicalID
	}

	return strings.TrimSpace(contentID)
}
