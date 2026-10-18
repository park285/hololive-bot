package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

func LoadWorkerProfile() (*AlarmWorkerProfile, error) {
	loaded, err := workercontract.LoadProfileFromEnv("hololive", "alarm-worker")
	if err != nil {
		return nil, fmt.Errorf("load stack worker profile: %w", err)
	}

	profile := &AlarmWorkerProfile{Loaded: loaded}
	if err := workercontract.DecodeWorkerSettingsSet(loaded, map[string]any{
		WorkerAlarmDispatch:        &profile.AlarmDispatch,
		WorkerNotificationDelivery: &profile.NotificationDelivery,
		WorkerYouTubeDelivery:      &profile.YouTubeDelivery,
	}); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := validateWorkerProfile(profile); err != nil {
		return nil, fmt.Errorf("validate alarm worker profile: %w", err)
	}

	return profile, nil
}

func validateWorkerProfile(profile *AlarmWorkerProfile) error {
	if profile == nil {
		return errors.New("alarm worker profile is nil")
	}

	workers := profile.Loaded.Profile.Workers
	problems := workercontract.ShapeProblems(workers, map[string]workercontract.WorkerShape{
		WorkerAlarmDispatch:        {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeNone},
		WorkerNotificationDelivery: {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeNone},
		WorkerYouTubeDelivery:      {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeFixed},
	})

	problems = append(problems, positiveValueProblems(profile)...)
	problems = append(problems, positiveIntProblems(profile)...)
	problems = append(problems, relationshipProblems(profile, workers)...)

	if err := runtimepolicy.JoinWorkerProfileProblems("alarm-worker", problems); err != nil {
		return fmt.Errorf("join worker profile problems: %w", err)
	}

	return nil
}

func positiveValueProblems(profile *AlarmWorkerProfile) []string {
	return runtimepolicy.PositiveValueProblems(map[string]int64{
		"alarm_dispatch.lease_ms":                               profile.AlarmDispatch.LeaseMS,
		"alarm_dispatch.quarantine_threshold_ms":                profile.AlarmDispatch.QuarantineThresholdMS,
		"alarm_dispatch.recovery_interval_ms":                   profile.AlarmDispatch.RecoveryIntervalMS,
		"alarm_dispatch.poll_interval_ms":                       profile.AlarmDispatch.PollIntervalMS,
		"alarm_dispatch.idle_backoff_min_ms":                    profile.AlarmDispatch.IdleBackoffMinMS,
		"alarm_dispatch.idle_backoff_max_ms":                    profile.AlarmDispatch.IdleBackoffMaxMS,
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
		"youtube_delivery.aggregate_sync_interval_ms":           profile.YouTubeDelivery.AggregateSyncIntervalMS,
		"youtube_delivery.telemetry_poll_interval_ms":           profile.YouTubeDelivery.TelemetryPollIntervalMS,
		"youtube_delivery.telemetry_retry_backoff_ms":           profile.YouTubeDelivery.TelemetryRetryBackoffMS,
		"youtube_delivery.telemetry_retention_ms":               profile.YouTubeDelivery.TelemetryRetentionMS,
	})
}

func positiveIntProblems(profile *AlarmWorkerProfile) []string {
	return runtimepolicy.PositiveIntProblems(map[string]int{
		"alarm_dispatch.recovery_batch_size":              profile.AlarmDispatch.RecoveryBatchSize,
		"alarm_dispatch.max_batch":                        profile.AlarmDispatch.MaxBatch,
		"alarm_dispatch.max_batches_per_wake":             profile.AlarmDispatch.MaxBatchesPerWake,
		"notification_delivery.batch_size":                profile.NotificationDelivery.BatchSize,
		"notification_delivery.max_retries":               profile.NotificationDelivery.MaxRetries,
		"notification_delivery.stale_sending_sweep_limit": profile.NotificationDelivery.StaleSendingSweepLimit,
		"youtube_delivery.batch_size":                     profile.YouTubeDelivery.BatchSize,
		"youtube_delivery.max_retries":                    profile.YouTubeDelivery.MaxRetries,
		"youtube_delivery.subscriber_lookup_parallelism":  profile.YouTubeDelivery.SubscriberLookupParallelism,
		"youtube_delivery.telemetry_flush_batch":          profile.YouTubeDelivery.TelemetryFlushBatch,
	})
}

func relationshipProblems(profile *AlarmWorkerProfile, workers map[string]workercontract.WorkerProfile) []string {
	problems := make([]string, 0)

	if profile.AlarmDispatch.IdleBackoffMaxMS < profile.AlarmDispatch.IdleBackoffMinMS {
		problems = append(problems, "alarm_dispatch idle backoff range is invalid")
	}

	freshness := workers[WorkerYouTubeDelivery].Queue.MaxAge.Milliseconds
	if freshness != nil && (*freshness < profile.YouTubeDelivery.ReviveFreshnessWindowMS ||
		*freshness-profile.YouTubeDelivery.ReviveFreshnessWindowMS < profile.YouTubeDelivery.ReviveIntervalMS) {
		problems = append(problems, "youtube_delivery max_age must cover revive freshness plus interval")
	}

	alarm := workers[WorkerAlarmDispatch]
	if alarm.Executor.Enabled && alarm.Executor.ConfiguredWorkers != 1 {
		problems = append(problems, "alarm_dispatch currently requires exactly one scheduler worker")
	}

	if timeout := alarm.Executor.AttemptTimeout.Milliseconds; timeout != nil && *timeout >= profile.AlarmDispatch.LeaseMS {
		problems = append(problems, "alarm_dispatch attempt_timeout must be less than lease_ms")
	}

	notificationTimeout := workers[WorkerNotificationDelivery].Executor.AttemptTimeout.Milliseconds
	if notificationTimeout != nil {
		if err := ValidateNotificationDeliveryTiming(
			workercontract.Milliseconds(*notificationTimeout),
			workercontract.Milliseconds(profile.NotificationDelivery.StaleSendingAfterMS),
		); err != nil {
			problems = append(problems, "notification_delivery: "+err.Error())
		}
	}

	return problems
}

// ValidateNotificationDeliveryTiming은 프로필 사전 검사와 프로필 없이 생성하는 dispatcher가
// 같은 lease·정산·stale 정책을 적용하게 한다.
func ValidateNotificationDeliveryTiming(attemptTimeout, staleSendingAfter time.Duration) error {
	if attemptTimeout <= 0 || attemptTimeout >= NotificationDeliveryLease-NotificationDeliveryFinalizeTimeout {
		return fmt.Errorf("attempt timeout must be positive and leave finalization budget within %s lease", NotificationDeliveryLease)
	}

	// stale SENDING sweep은 lease 만료를 보지 않으므로 진행 중인 발송을 격리하지 않도록
	// sending_started_at 기준 임계값이 SENDING lease 전체를 덮어야 한다.
	if staleSendingAfter < NotificationDeliveryLease {
		return fmt.Errorf("stale sending after must be at least the %s sending lease", NotificationDeliveryLease)
	}

	return nil
}
