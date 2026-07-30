package dispatch

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	timeline "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
)

func buildDeliveryAuditLogAttrs(row *domain.YouTubeNotificationDeliveryTelemetry) []any {
	classification := timeline.PostLatencyClassificationResult{}
	return buildDeliveryAuditLogAttrsWithClassification(row, &classification)
}
