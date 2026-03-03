package delivery

import "github.com/kapu/hololive-shared/pkg/domain"

const (
	// OutboxPayloadVersionV1: 현재 outbox payload 버전 (payload 내에 version 필드가 포함되지는 않음)
	OutboxPayloadVersionV1 uint8 = 1
)

const (
	// Kind*: 도메인 값과 동일한 outbox kind (string contract)
	KindMajorEventWeekly  domain.DeliveryOutboxKind = domain.DeliveryKindMajorEventWeekly
	KindMajorEventMonthly domain.DeliveryOutboxKind = domain.DeliveryKindMajorEventMonthly
	KindMemberNewsWeekly  domain.DeliveryOutboxKind = domain.DeliveryKindMemberNewsWeekly
	KindMemberNewsMonthly domain.DeliveryOutboxKind = domain.DeliveryKindMemberNewsMonthly
)

const (
	StatusPending domain.DeliveryOutboxStatus = domain.DeliveryStatusPending
	StatusSent    domain.DeliveryOutboxStatus = domain.DeliveryStatusSent
	StatusFailed  domain.DeliveryOutboxStatus = domain.DeliveryStatusFailed
)

// OutboxPayloadV1: outbox에 저장되는 payload 스키마.
//
// 현재 구현(hololive-shared/pkg/service/delivery/outbox_repository.go)의 JSON({ "message": "..." })과 동일합니다.
type OutboxPayloadV1 struct {
	Message string `json:"message"`
}

// ContentID: outbox content_id 생성 규약(SSOT).
//
// 현재 구현은 periodKey + ":" + roomID 입니다.
func ContentID(periodKey, roomID string) string {
	return periodKey + ":" + roomID
}
