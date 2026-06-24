package telemetry

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func deliveryTelemetryIdentityForRow(row *domain.YouTubeNotificationDeliveryTelemetry) (deliveryTelemetryIdentity, bool) {
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
	case domain.AlarmTypeLive, domain.AlarmTypeBirthday, domain.AlarmTypeAnniversary:
		return "", false
	default:
		return "", false
	}
}
