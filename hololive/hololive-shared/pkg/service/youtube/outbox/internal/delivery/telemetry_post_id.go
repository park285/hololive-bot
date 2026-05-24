package delivery

import (
	"strings"

	json "github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func normalizeTelemetryPostID(value string) string {
	return strings.TrimSpace(value)
}

func resolveTelemetryPostID(kind domain.OutboxKind, contentID, payload string) string {
	switch kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		return resolveVideoTelemetryPostID(contentID, payload)
	case domain.OutboxKindCommunityPost:
		return resolveCommunityTelemetryPostID(contentID, payload)
	}

	return normalizeTelemetryPostID(contentID)
}

func resolveVideoTelemetryPostID(contentID, payload string) string {
	var parsed videoPayload
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return normalizeTelemetryPostID(contentID)
	}

	return firstTelemetryPostID(parsed.CanonicalPostID, contentID, parsed.VideoID)
}

func resolveCommunityTelemetryPostID(contentID, payload string) string {
	var parsed communityPayload
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return normalizeTelemetryPostID(contentID)
	}

	return firstTelemetryPostID(parsed.CanonicalPostID, contentID, parsed.PostID)
}

func firstTelemetryPostID(values ...string) string {
	for _, value := range values {
		if postID := normalizeTelemetryPostID(value); postID != "" {
			return postID
		}
	}

	return ""
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
