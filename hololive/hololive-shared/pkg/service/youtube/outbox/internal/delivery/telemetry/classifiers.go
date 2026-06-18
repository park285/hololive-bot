package telemetry

import (
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const CommunityShortsDeliveryPath = "youtube_outbox_dispatcher"

func DedupeKeyLogValue(outbox *domain.YouTubeNotificationOutbox) string {
	dedupeKey, err := outbox.DedupeKey()
	if err == nil {
		return dedupeKey
	}

	return fmt.Sprintf("invalid:%s:%s",
		strings.TrimSpace(string(outbox.Kind)),
		strings.TrimSpace(outbox.ContentID),
	)
}

func NormalizeCommunityShortsDeliveryPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return CommunityShortsDeliveryPath
	}
	return trimmed
}

func IsCommunityShortsDeliveryAuditKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindNewShort, domain.OutboxKindCommunityPost:
		return true
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
		return false
	default:
		return false
	}
}
