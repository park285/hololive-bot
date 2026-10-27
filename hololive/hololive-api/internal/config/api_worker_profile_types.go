package config

import "github.com/park285/shared-go/v2/pkg/workercontract"

const (
	WorkerBotWebhookInbox   = "bot_webhook_inbox"
	WorkerBotReplyOutbox    = "bot_reply_outbox"
	WorkerSourceObservation = "source_observation"
)

// APIWorkerIdentity: hololive-api가 소유한 worker profile identity다.
// 호출자가 WorkerIDs를 바꿔도 다른 로드에 번지지 않도록 매 호출 새 슬라이스를 돌려준다.
func APIWorkerIdentity() workercontract.Identity {
	return workercontract.Identity{
		Service:   "hololive",
		Role:      "api",
		WorkerIDs: []string{WorkerBotReplyOutbox, WorkerBotWebhookInbox, WorkerSourceObservation},
	}
}

type BotWebhookInboxWorkerSettings struct {
	MaxBodyBytes            int64 `json:"max_body_bytes"`
	DedupTimeoutMS          int64 `json:"dedup_timeout_ms"`
	PollIntervalMS          int64 `json:"poll_interval_ms"`
	ClaimLeaseMS            int64 `json:"claim_lease_ms"`
	HeartbeatIntervalMS     int64 `json:"heartbeat_interval_ms"`
	OwnershipSafetyMarginMS int64 `json:"ownership_safety_margin_ms"`
	RetryAfterMS            int64 `json:"retry_after_ms"`
	MaxAttempts             int32 `json:"max_attempts"`
	MaintenanceIntervalMS   int64 `json:"maintenance_interval_ms"`
	SettlementTimeoutMS     int64 `json:"settlement_timeout_ms"`
	TerminalRetentionMS     int64 `json:"terminal_retention_ms"`
}

type BotReplyOutboxWorkerSettings struct {
	PollIntervalMS           int64 `json:"poll_interval_ms"`
	ClaimLeaseMS             int64 `json:"claim_lease_ms"`
	RetryAfterMS             int64 `json:"retry_after_ms"`
	MaxAttempts              int32 `json:"max_attempts"`
	MaintenanceIntervalMS    int64 `json:"maintenance_interval_ms"`
	ManualReviewRetentionMS  int64 `json:"manual_review_retention_ms"`
	AutomaticReplayHorizonMS int64 `json:"automatic_replay_horizon_ms"`
}

type SourceObservationWorkerSettings struct {
	DBOperationConcurrency int   `json:"db_operation_concurrency"`
	ClaimBatchSize         int   `json:"claim_batch_size"`
	ClaimIntervalMS        int64 `json:"claim_interval_ms"`
	ClaimLeaseMS           int64 `json:"claim_lease_ms"`
	ShutdownTimeoutMS      int64 `json:"shutdown_timeout_ms"`
}

type APIWorkerProfile struct {
	Loaded            workercontract.LoadedProfile
	BotWebhookInbox   BotWebhookInboxWorkerSettings
	BotReplyOutbox    BotReplyOutboxWorkerSettings
	SourceObservation SourceObservationWorkerSettings
}
