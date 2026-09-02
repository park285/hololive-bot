package alarmworker

import (
	"errors"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

func LoadWorkerProfile() (*settings.AlarmWorkerProfile, error) {
	loaded, err := workercontract.LoadProfileFromEnv("hololive", "alarm-worker")
	if err != nil {
		return nil, fmt.Errorf("load stack worker profile: %w", err)
	}

	profile := &settings.AlarmWorkerProfile{Loaded: loaded}
	if err := workercontract.DecodeWorkerSettings(loaded, "alarm_dispatch", &profile.AlarmDispatch); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := workercontract.DecodeWorkerSettings(loaded, "notification_delivery", &profile.NotificationDelivery); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := workercontract.DecodeWorkerSettings(loaded, "youtube_delivery", &profile.YouTubeDelivery); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := validateWorkerProfile(profile); err != nil {
		return nil, fmt.Errorf("validate alarm worker profile: %w", err)
	}

	return profile, nil
}

func validateWorkerProfile(profile *settings.AlarmWorkerProfile) error {
	if profile == nil {
		return errors.New("alarm worker profile is nil")
	}

	workers := profile.Loaded.Profile.Workers
	problems := workercontract.ShapeProblems(workers, map[string]workercontract.WorkerShape{
		"alarm_dispatch":        {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeFixed},
		"notification_delivery": {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeFixed},
		"youtube_delivery":      {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeFixed},
	})

	problems = append(problems, positiveValueProblems(profile)...)
	problems = append(problems, positiveIntProblems(profile)...)
	problems = append(problems, relationshipProblems(profile, workers)...)

	if err := load.JoinWorkerProfileProblems("alarm-worker", problems); err != nil {
		return fmt.Errorf("join worker profile problems: %w", err)
	}

	return nil
}

func positiveValueProblems(profile *settings.AlarmWorkerProfile) []string {
	return load.PositiveValueProblems(map[string]int64{
		"alarm_dispatch.lease_ms":                               profile.AlarmDispatch.LeaseMS,
		"alarm_dispatch.quarantine_threshold_ms":                profile.AlarmDispatch.QuarantineThresholdMS,
		"alarm_dispatch.recovery_interval_ms":                   profile.AlarmDispatch.RecoveryIntervalMS,
		"alarm_dispatch.poll_interval_ms":                       profile.AlarmDispatch.PollIntervalMS,
		"alarm_dispatch.idle_backoff_min_ms":                    profile.AlarmDispatch.IdleBackoffMinMS,
		"alarm_dispatch.idle_backoff_max_ms":                    profile.AlarmDispatch.IdleBackoffMaxMS,
		"notification_delivery.lock_timeout_ms":                 profile.NotificationDelivery.LockTimeoutMS,
		"notification_delivery.poll_interval_ms":                profile.NotificationDelivery.PollIntervalMS,
		"notification_delivery.retry_backoff_ms":                profile.NotificationDelivery.RetryBackoffMS,
		"notification_delivery.cleanup_after_ms":                profile.NotificationDelivery.CleanupAfterMS,
		"notification_delivery.cleanup_interval_ms":             profile.NotificationDelivery.CleanupIntervalMS,
		"notification_delivery.stale_sending_after_ms":          profile.NotificationDelivery.StaleSendingAfterMS,
		"notification_delivery.stale_sending_sweep_interval_ms": profile.NotificationDelivery.StaleSendingSweepIntervalMS,
		"youtube_delivery.lock_timeout_ms":                      profile.YouTubeDelivery.LockTimeoutMS,
		"youtube_delivery.poll_interval_ms":                     profile.YouTubeDelivery.PollIntervalMS,
		"youtube_delivery.retry_backoff_ms":                     profile.YouTubeDelivery.RetryBackoffMS,
		"youtube_delivery.cleanup_after_ms":                     profile.YouTubeDelivery.CleanupAfterMS,
		"youtube_delivery.revive_interval_ms":                   profile.YouTubeDelivery.ReviveIntervalMS,
		"youtube_delivery.revive_freshness_window_ms":           profile.YouTubeDelivery.ReviveFreshnessWindowMS,
		"youtube_delivery.claim_freshness_window_ms":            profile.YouTubeDelivery.ClaimFreshnessWindowMS,
		"youtube_delivery.delivery_send_timeout_ms":             profile.YouTubeDelivery.DeliverySendTimeoutMS,
		"youtube_delivery.aggregate_sync_interval_ms":           profile.YouTubeDelivery.AggregateSyncIntervalMS,
		"youtube_delivery.telemetry_poll_interval_ms":           profile.YouTubeDelivery.TelemetryPollIntervalMS,
		"youtube_delivery.telemetry_retry_backoff_ms":           profile.YouTubeDelivery.TelemetryRetryBackoffMS,
		"youtube_delivery.telemetry_retention_ms":               profile.YouTubeDelivery.TelemetryRetentionMS,
	})
}

func positiveIntProblems(profile *settings.AlarmWorkerProfile) []string {
	return load.PositiveIntProblems(map[string]int{
		"alarm_dispatch.recovery_batch_size":              profile.AlarmDispatch.RecoveryBatchSize,
		"alarm_dispatch.max_batch":                        profile.AlarmDispatch.MaxBatch,
		"alarm_dispatch.max_batches_per_wake":             profile.AlarmDispatch.MaxBatchesPerWake,
		"notification_delivery.batch_size":                profile.NotificationDelivery.BatchSize,
		"notification_delivery.max_retries":               profile.NotificationDelivery.MaxRetries,
		"notification_delivery.stale_sending_sweep_limit": profile.NotificationDelivery.StaleSendingSweepLimit,
		"youtube_delivery.batch_size":                     profile.YouTubeDelivery.BatchSize,
		"youtube_delivery.max_retries":                    profile.YouTubeDelivery.MaxRetries,
		"youtube_delivery.subscriber_lookup_parallelism":  profile.YouTubeDelivery.SubscriberLookupParallelism,
		"youtube_delivery.telemetry_backfill_batch":       profile.YouTubeDelivery.TelemetryBackfillBatch,
		"youtube_delivery.telemetry_flush_batch":          profile.YouTubeDelivery.TelemetryFlushBatch,
	})
}

func relationshipProblems(profile *settings.AlarmWorkerProfile, workers map[string]workercontract.WorkerProfile) []string {
	problems := make([]string, 0)

	if profile.AlarmDispatch.IdleBackoffMaxMS < profile.AlarmDispatch.IdleBackoffMinMS {
		problems = append(problems, "alarm_dispatch idle backoff range is invalid")
	}

	if profile.YouTubeDelivery.ClaimFreshnessWindowMS < profile.YouTubeDelivery.ReviveFreshnessWindowMS+profile.YouTubeDelivery.ReviveIntervalMS {
		problems = append(problems, "youtube_delivery claim freshness window is invalid")
	}

	alarm := workers["alarm_dispatch"]
	if alarm.Executor.Enabled && alarm.Executor.ConfiguredWorkers != 1 {
		problems = append(problems, "alarm_dispatch currently requires exactly one scheduler worker")
	}

	return problems
}
