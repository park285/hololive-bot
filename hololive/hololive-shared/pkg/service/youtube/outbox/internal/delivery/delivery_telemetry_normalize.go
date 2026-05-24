package delivery

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func normalizeDeliveryTelemetryObservationStatus(status string) string {
	normalized := strings.TrimSpace(status)
	if normalized == "" {
		return deliveryTelemetryObservationStatusUnclassified
	}
	return normalized
}

func deliveryTelemetryIdentityForRow(row domain.YouTubeNotificationDeliveryTelemetry) (deliveryTelemetryIdentity, bool) {
	kind, ok := deliveryTelemetryKindForAlarmType(row.AlarmType)
	if !ok {
		return deliveryTelemetryIdentity{}, false
	}

	contentID := strings.TrimSpace(row.ContentID)
	if contentID == "" {
		return deliveryTelemetryIdentity{}, false
	}

	return deliveryTelemetryIdentity{kind: kind, contentID: contentID}, true
}

func deliveryTelemetryKindForAlarmType(alarmType domain.AlarmType) (domain.OutboxKind, bool) {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		return domain.OutboxKindCommunityPost, true
	case domain.AlarmTypeShorts:
		return domain.OutboxKindNewShort, true
	default:
		return "", false
	}
}
