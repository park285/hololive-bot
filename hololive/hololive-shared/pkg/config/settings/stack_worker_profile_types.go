package settings

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

type AlarmDispatchWorkerSettings struct {
	LeaseMS               int64 `json:"lease_ms"`
	QuarantineThresholdMS int64 `json:"quarantine_threshold_ms"`
	RecoveryIntervalMS    int64 `json:"recovery_interval_ms"`
	RecoveryBatchSize     int   `json:"recovery_batch_size"`
	MaxBatch              int   `json:"max_batch"`
	MaxBatchesPerWake     int   `json:"max_batches_per_wake"`
	PollIntervalMS        int64 `json:"poll_interval_ms"`
	IdleBackoffMinMS      int64 `json:"idle_backoff_min_ms"`
	IdleBackoffMaxMS      int64 `json:"idle_backoff_max_ms"`
	WakeupEnabled         bool  `json:"wakeup_enabled"`
}

type NotificationDeliveryWorkerSettings struct {
	BatchSize                   int   `json:"batch_size"`
	MaxRetries                  int   `json:"max_retries"`
	LockTimeoutMS               int64 `json:"lock_timeout_ms"`
	PollIntervalMS              int64 `json:"poll_interval_ms"`
	RetryBackoffMS              int64 `json:"retry_backoff_ms"`
	CleanupAfterMS              int64 `json:"cleanup_after_ms"`
	CleanupIntervalMS           int64 `json:"cleanup_interval_ms"`
	CleanupEnabled              bool  `json:"cleanup_enabled"`
	StaleSendingAfterMS         int64 `json:"stale_sending_after_ms"`
	StaleSendingSweepIntervalMS int64 `json:"stale_sending_sweep_interval_ms"`
	StaleSendingSweepLimit      int   `json:"stale_sending_sweep_limit"`
}

type YouTubeDeliveryWorkerSettings struct {
	BatchSize                   int   `json:"batch_size"`
	LockTimeoutMS               int64 `json:"lock_timeout_ms"`
	PollIntervalMS              int64 `json:"poll_interval_ms"`
	MaxRetries                  int   `json:"max_retries"`
	RetryBackoffMS              int64 `json:"retry_backoff_ms"`
	CleanupAfterMS              int64 `json:"cleanup_after_ms"`
	CleanupEnabled              bool  `json:"cleanup_enabled"`
	ReviveEnabled               bool  `json:"revive_enabled"`
	ReviveIntervalMS            int64 `json:"revive_interval_ms"`
	ReviveFreshnessWindowMS     int64 `json:"revive_freshness_window_ms"`
	ClaimFreshnessWindowMS      int64 `json:"claim_freshness_window_ms"`
	DeliverySendTimeoutMS       int64 `json:"delivery_send_timeout_ms"`
	SubscriberLookupParallelism int   `json:"subscriber_lookup_parallelism"`
	AggregateSyncIntervalMS     int64 `json:"aggregate_sync_interval_ms"`
	TelemetryPollIntervalMS     int64 `json:"telemetry_poll_interval_ms"`
	TelemetryBackfillBatch      int   `json:"telemetry_backfill_batch"`
	TelemetryFlushBatch         int   `json:"telemetry_flush_batch"`
	TelemetryRetryBackoffMS     int64 `json:"telemetry_retry_backoff_ms"`
	TelemetryRetentionMS        int64 `json:"telemetry_retention_ms"`
}

type AlarmWorkerProfile struct {
	Loaded               workercontract.LoadedProfile
	AlarmDispatch        AlarmDispatchWorkerSettings
	NotificationDelivery NotificationDeliveryWorkerSettings
	YouTubeDelivery      YouTubeDeliveryWorkerSettings
}

type CollectionWorkerSettings struct {
	AcquisitionBatch           int   `json:"acquisition_batch"`
	AcquisitionCadenceMS       int64 `json:"acquisition_cadence_ms"`
	LeaseTTLMS                 int64 `json:"lease_ttl_ms"`
	RenewIntervalMS            int64 `json:"renew_interval_ms"`
	RenewTimeoutMS             int64 `json:"renew_timeout_ms"`
	DBTimeoutMS                int64 `json:"db_timeout_ms"`
	CleanupTimeoutMS           int64 `json:"cleanup_timeout_ms"`
	ProviderAdmissionTimeoutMS int64 `json:"provider_admission_timeout_ms"`
	CollectionOverheadMS       int64 `json:"collection_overhead_ms"`
	PublishTimeoutMS           int64 `json:"publish_timeout_ms"`
	RetryMinMS                 int64 `json:"retry_min_ms"`
	RetryMaxMS                 int64 `json:"retry_max_ms"`
	ReleaseJitterMinMS         int64 `json:"release_jitter_min_ms"`
	ReleaseJitterMaxMS         int64 `json:"release_jitter_max_ms"`
	HolodexMaxInflight         int   `json:"holodex_max_inflight"`
	OfficialMaxInflight        int   `json:"official_max_inflight"`
	YouTubeJSMaxInflight       int   `json:"youtubejs_max_inflight"`
}

type YouTubeCollectorWorkerProfile struct {
	Loaded     workercontract.LoadedProfile
	Collection CollectionWorkerSettings
}
