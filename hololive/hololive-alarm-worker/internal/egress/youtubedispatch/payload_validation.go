package youtubedispatch

import (
	jsonv2 "encoding/json/v2"
	"strings"

	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func validateOutboxPayload(item *domain.YouTubeNotificationOutbox) bool {
	switch item.Kind {
	case domain.OutboxKindNewVideo:
		payload, ok := decodeOutboxPayload[youtubeoutbox.Video](item.Payload)

		return ok && hasGroupedVideoFields(&payload.VideoFields, item.ContentID)
	case domain.OutboxKindNewShort:
		payload, ok := decodeOutboxPayload[youtubeoutbox.Short](item.Payload)

		return ok && hasGroupedVideoFields(&payload.VideoFields, item.ContentID)
	case domain.OutboxKindCommunityPost:
		payload, ok := decodeOutboxPayload[youtubeoutbox.Community](item.Payload)

		return ok && hasGroupedCommunityFields(&payload, item.ContentID)
	case domain.OutboxKindLiveStream:
		return true
	default:
		return true
	}
}

func hasGroupedVideoFields(payload *youtubeoutbox.VideoFields, contentID string) bool {
	return notBlank(payload.Title) && (notBlank(payload.VideoID) || notBlank(contentID))
}

func hasGroupedCommunityFields(payload *youtubeoutbox.Community, contentID string) bool {
	return notBlank(payload.ContentText) &&
		(notBlank(payload.CanonicalPostID) || notBlank(payload.PostID) || notBlank(contentID))
}

func decodeOutboxPayload[T any](raw string) (T, bool) {
	var payload T

	if err := jsonv2.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, false
	}

	return payload, true
}

func notBlank(value string) bool {
	return strings.TrimSpace(value) != ""
}
