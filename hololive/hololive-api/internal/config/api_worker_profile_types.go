package config

import "github.com/park285/shared-go/v2/pkg/workercontract"

type BotWebhookInboxWorkerSettings struct {
	MaxBodyBytes            int64 `json:"max_body_bytes"`
	DedupTTLMS              int64 `json:"dedup_ttl_ms"`
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
	DispatchBudgetMS         int64 `json:"dispatch_budget_ms"`
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
	TransactionTimeoutMS   int64 `json:"transaction_timeout_ms"`
	ShutdownTimeoutMS      int64 `json:"shutdown_timeout_ms"`
}

type APIWorkerProfile struct {
	Loaded            workercontract.LoadedProfile
	BotWebhookInbox   BotWebhookInboxWorkerSettings
	BotReplyOutbox    BotReplyOutboxWorkerSettings
	SourceObservation SourceObservationWorkerSettings
}
