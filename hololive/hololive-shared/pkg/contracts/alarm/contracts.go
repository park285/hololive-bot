package alarm

import "github.com/kapu/hololive-shared/pkg/domain"

const (
	// DispatchQueueKey: Rust alarm service -> dispatcher Valkey queue key
	DispatchQueueKey = "alarm:dispatch:queue"

	// NotifyClaimKeyPrefix: dedup claim key prefix
	NotifyClaimKeyPrefix = "notified:claim:"
	// NotifyLogicalClaimKeyPrefix: logical-event dedup claim key prefix
	NotifyLogicalClaimKeyPrefix = "notified:claim:event:"

	// QueueEnvelopeVersionV1: current alarm queue envelope version
	QueueEnvelopeVersionV1 uint8 = 1
)

// AlarmQueueEnvelope: cross-language queue envelope contract
type AlarmQueueEnvelope struct {
	Notification domain.AlarmNotification `json:"notification"`
	ClaimKeys    []string                 `json:"claim_keys"`
	EnqueuedAt   string                   `json:"enqueued_at"`
	Version      uint8                    `json:"version"`
}
