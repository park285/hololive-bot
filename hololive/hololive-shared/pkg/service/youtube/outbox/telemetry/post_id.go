package telemetry

import (
	"strings"

	jsonv2 "encoding/json/v2"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/format"
)

func normalizeTelemetryPostID(value string) string {
	return strings.TrimSpace(value)
}

func ResolveTelemetryPostID(kind domain.OutboxKind, contentID, payload string) string {
	switch kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		return resolveVideoTelemetryPostID(contentID, payload)
	case domain.OutboxKindCommunityPost:
		return resolveCommunityTelemetryPostID(contentID, payload)
	case domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
		return normalizeTelemetryPostID(contentID)
	}

	return normalizeTelemetryPostID(contentID)
}

func resolveVideoTelemetryPostID(contentID, payload string) string {
	var parsed format.VideoPayload
	if err := jsonv2.Unmarshal([]byte(payload), &parsed); err != nil {
		return normalizeTelemetryPostID(contentID)
	}

	return firstTelemetryPostID(parsed.CanonicalPostID, contentID, parsed.VideoID)
}

func resolveCommunityTelemetryPostID(contentID, payload string) string {
	var parsed format.CommunityPayload
	if err := jsonv2.Unmarshal([]byte(payload), &parsed); err != nil {
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

func ApplyTelemetryPostID(row *domain.YouTubeNotificationDeliveryTelemetry) {
	if row == nil {
		return
	}

	row.ContentID = normalizeTelemetryPostID(row.ContentID)
	row.PostID = normalizeTelemetryPostID(row.PostID)
	if row.PostID == "" {
		row.PostID = row.ContentID
	}
}
