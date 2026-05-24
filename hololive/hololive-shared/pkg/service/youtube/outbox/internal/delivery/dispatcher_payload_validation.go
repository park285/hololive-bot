package delivery

import (
	"strings"

	json "github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func validateOutboxPayload(item domain.YouTubeNotificationOutbox) bool {
	switch item.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		return validateVideoOutboxPayload(item)
	case domain.OutboxKindCommunityPost:
		return validateCommunityOutboxPayload(item)
	default:
		return true
	}
}

func validateVideoOutboxPayload(item domain.YouTubeNotificationOutbox) bool {
	raw, ok := decodeOutboxPayloadMap(item.Payload)
	return ok &&
		payloadString(raw, "title") != "" &&
		(payloadString(raw, "video_id") != "" ||
			payloadString(raw, "url") != "" ||
			strings.TrimSpace(item.ContentID) != "")
}

func validateCommunityOutboxPayload(item domain.YouTubeNotificationOutbox) bool {
	raw, ok := decodeOutboxPayloadMap(item.Payload)
	return ok &&
		(payloadString(raw, "content_text") != "" || payloadString(raw, "url") != "") &&
		(payloadString(raw, "canonical_post_id") != "" ||
			payloadString(raw, "post_id") != "" ||
			strings.TrimSpace(item.ContentID) != "")
}

func decodeOutboxPayloadMap(payload string) (map[string]any, bool) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return nil, false
	}

	return raw, true
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}

	str, ok := value.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(str)
}
