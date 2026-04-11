package outbox

import (
	"strings"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func normalizeTelemetryPostID(value string) string {
	return strings.TrimSpace(value)
}

func resolveTelemetryPostID(kind domain.OutboxKind, contentID, payload string) string {
	switch kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		var parsed videoPayload
		if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
			if postID := normalizeTelemetryPostID(parsed.CanonicalPostID); postID != "" {
				return postID
			}
			if postID := normalizeTelemetryPostID(contentID); postID != "" {
				return postID
			}
			if postID := normalizeTelemetryPostID(parsed.VideoID); postID != "" {
				return postID
			}
		}
	case domain.OutboxKindCommunityPost:
		var parsed communityPayload
		if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
			if postID := normalizeTelemetryPostID(parsed.CanonicalPostID); postID != "" {
				return postID
			}
			if postID := normalizeTelemetryPostID(contentID); postID != "" {
				return postID
			}
			if postID := normalizeTelemetryPostID(parsed.PostID); postID != "" {
				return postID
			}
		}
	}

	return normalizeTelemetryPostID(contentID)
}

func applyTelemetryPostID(row *domain.YouTubeNotificationDeliveryTelemetry) {
	if row == nil {
		return
	}

	row.ContentID = normalizeTelemetryPostID(row.ContentID)
	row.PostID = normalizeTelemetryPostID(row.PostID)
	if row.PostID == "" {
		row.PostID = row.ContentID
	}
}
