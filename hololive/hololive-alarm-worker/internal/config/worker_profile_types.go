package config

import (
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

const (
	WorkerAlarmDispatch        = "alarm_dispatch"
	WorkerNotificationDelivery = "notification_delivery"
	WorkerYouTubeDelivery      = "youtube_delivery"
)

// WorkerIdentity: hololive-alarm-worker가 소유한 worker profile identity다.
// 호출자가 WorkerIDs를 바꿔도 다른 로드에 번지지 않도록 매 호출 새 슬라이스를 돌려준다.
func WorkerIdentity() workercontract.Identity {
	return workercontract.Identity{
		Service:   "hololive",
		Role:      "alarm-worker",
		WorkerIDs: []string{WorkerAlarmDispatch, WorkerNotificationDelivery, WorkerYouTubeDelivery},
	}
}

// NotificationDeliveryLease는 claim과 SENDING 전이에 사용하는 소유권 예산이다.
const NotificationDeliveryLease = 60 * time.Second

// NotificationDeliveryFinalizeTimeout은 발송 결과 저장에 예약하는 독립 예산이다.
const NotificationDeliveryFinalizeTimeout = 5 * time.Second

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
	SubscriberLookupParallelism int   `json:"subscriber_lookup_parallelism"`
	AggregateSyncIntervalMS     int64 `json:"aggregate_sync_interval_ms"`
	TelemetryPollIntervalMS     int64 `json:"telemetry_poll_interval_ms"`
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
