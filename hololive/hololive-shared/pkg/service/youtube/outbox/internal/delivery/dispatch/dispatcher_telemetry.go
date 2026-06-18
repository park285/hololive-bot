package dispatch

import "github.com/kapu/hololive-shared/pkg/domain"

func buildDeliveryAuditLogAttrs(row *domain.YouTubeNotificationDeliveryTelemetry) []any {
	classification := PostLatencyClassificationResult{}
	return buildDeliveryAuditLogAttrsWithClassification(row, &classification)
}
