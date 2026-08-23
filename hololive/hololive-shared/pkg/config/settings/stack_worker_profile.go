package settings

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

const StackWorkerProfileFileEnv = "STACK_WORKER_PROFILE_FILE"

func LoadAPIWorkerProfile() (*APIWorkerProfile, error) {
	loaded, err := loadStackWorkerProfile("hololive", "api")
	if err != nil {
		return nil, err
	}
	profile := &APIWorkerProfile{Loaded: loaded}
	if err := decodeWorkerSettings(&loaded, "bot_webhook_inbox", &profile.BotWebhookInbox,
		"max_body_bytes", "dedup_ttl_ms", "dedup_timeout_ms", "poll_interval_ms", "claim_lease_ms",
		"heartbeat_interval_ms", "ownership_safety_margin_ms", "retry_after_ms", "max_attempts",
		"maintenance_interval_ms", "settlement_timeout_ms", "terminal_retention_ms"); err != nil {
		return nil, err
	}
	if err := decodeWorkerSettings(&loaded, "bot_reply_outbox", &profile.BotReplyOutbox,
		"poll_interval_ms", "claim_lease_ms", "dispatch_budget_ms", "retry_after_ms", "max_attempts",
		"maintenance_interval_ms", "manual_review_retention_ms", "automatic_replay_horizon_ms"); err != nil {
		return nil, err
	}
	if err := decodeWorkerSettings(&loaded, "source_observation", &profile.SourceObservation,
		"db_operation_concurrency", "claim_batch_size", "claim_interval_ms", "claim_lease_ms",
		"transaction_timeout_ms", "shutdown_timeout_ms"); err != nil {
		return nil, err
	}
	if err := validateAPIWorkerProfile(profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func LoadAlarmWorkerProfile() (*AlarmWorkerProfile, error) {
	loaded, err := loadStackWorkerProfile("hololive", "alarm-worker")
	if err != nil {
		return nil, err
	}
	profile := &AlarmWorkerProfile{Loaded: loaded}
	if err := decodeWorkerSettings(&loaded, "alarm_dispatch", &profile.AlarmDispatch,
		"lease_ms", "quarantine_threshold_ms", "recovery_interval_ms", "recovery_batch_size", "max_batch",
		"max_batches_per_wake", "poll_interval_ms", "idle_backoff_min_ms", "idle_backoff_max_ms", "wakeup_enabled"); err != nil {
		return nil, err
	}
	if err := decodeWorkerSettings(&loaded, "notification_delivery", &profile.NotificationDelivery,
		"batch_size", "max_retries", "lock_timeout_ms", "poll_interval_ms", "retry_backoff_ms", "cleanup_after_ms",
		"cleanup_interval_ms", "cleanup_enabled", "stale_sending_after_ms", "stale_sending_sweep_interval_ms",
		"stale_sending_sweep_limit"); err != nil {
		return nil, err
	}
	if err := decodeWorkerSettings(&loaded, "youtube_delivery", &profile.YouTubeDelivery,
		"batch_size", "lock_timeout_ms", "poll_interval_ms", "max_retries", "retry_backoff_ms", "cleanup_after_ms",
		"cleanup_enabled", "revive_enabled", "revive_interval_ms", "revive_freshness_window_ms",
		"claim_freshness_window_ms", "delivery_send_timeout_ms", "subscriber_lookup_parallelism",
		"aggregate_sync_interval_ms", "telemetry_poll_interval_ms", "telemetry_backfill_batch",
		"telemetry_flush_batch", "telemetry_retry_backoff_ms", "telemetry_retention_ms"); err != nil {
		return nil, err
	}
	if err := validateAlarmWorkerProfile(profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func LoadYouTubeCollectorWorkerProfile() (*YouTubeCollectorWorkerProfile, error) {
	loaded, err := loadStackWorkerProfile("hololive", "youtube-collector")
	if err != nil {
		return nil, err
	}
	profile := &YouTubeCollectorWorkerProfile{Loaded: loaded}
	if err := decodeWorkerSettings(&loaded, "collection", &profile.Collection,
		"acquisition_batch", "acquisition_cadence_ms", "lease_ttl_ms", "renew_interval_ms", "renew_timeout_ms",
		"db_timeout_ms", "cleanup_timeout_ms", "provider_admission_timeout_ms", "collection_overhead_ms",
		"publish_timeout_ms", "retry_min_ms", "retry_max_ms", "release_jitter_min_ms", "release_jitter_max_ms",
		"holodex_max_inflight", "official_max_inflight", "youtubejs_max_inflight"); err != nil {
		return nil, err
	}
	if err := validateCollectorWorkerProfile(profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func loadStackWorkerProfile(service, role string) (workercontract.LoadedProfile, error) {
	path, present := os.LookupEnv(StackWorkerProfileFileEnv)
	if !present || path == "" {
		return workercontract.LoadedProfile{}, errors.New("STACK_WORKER_PROFILE_FILE is required")
	}
	if path != strings.TrimSpace(path) {
		return workercontract.LoadedProfile{}, errors.New("STACK_WORKER_PROFILE_FILE must not contain surrounding whitespace")
	}
	identity, err := workercontract.KnownIdentity(service, role)
	if err != nil {
		return workercontract.LoadedProfile{}, err
	}
	loaded, err := workercontract.LoadProfileFile(path, identity)
	if err != nil {
		return workercontract.LoadedProfile{}, fmt.Errorf("load stack worker profile: %w", err)
	}
	return loaded, nil
}

func decodeWorkerSettings(loaded *workercontract.LoadedProfile, workerID string, destination any, requiredKeys ...string) error {
	if loaded == nil {
		return fmt.Errorf("decode %s settings: loaded profile is nil", workerID)
	}
	worker, ok := loaded.Profile.Workers[workerID]
	if !ok {
		return fmt.Errorf("decode %s settings: worker is missing", workerID)
	}
	if err := workercontract.DecodeSettings(worker.Settings, destination); err != nil {
		return fmt.Errorf("decode %s settings: %w", workerID, err)
	}
	var fields map[string]jsontext.Value
	if err := jsonv2.Unmarshal(worker.Settings, &fields); err != nil {
		return fmt.Errorf("decode %s settings keys: %w", workerID, err)
	}
	actual := make([]string, 0, len(fields))
	for key := range fields {
		actual = append(actual, key)
	}
	slices.Sort(actual)
	expected := slices.Clone(requiredKeys)
	slices.Sort(expected)
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("decode %s settings: got keys %v, want %v", workerID, actual, expected)
	}
	return nil
}

type workerShape struct {
	attemptTimeout workercontract.DurationMode
	capacity       workercontract.CapacityMode
	maxAge         workercontract.DurationMode
}

func validateAPIWorkerProfile(profile *APIWorkerProfile) error {
	if profile == nil {
		return errors.New("API worker profile is nil")
	}
	workers := profile.Loaded.Profile.Workers
	problems := validateWorkerShapes(workers, map[string]workerShape{
		"bot_webhook_inbox":  {workercontract.DurationModeFixed, workercontract.CapacityModeUnbounded, workercontract.DurationModeFixed},
		"bot_reply_outbox":   {workercontract.DurationModeFixed, workercontract.CapacityModeUnbounded, workercontract.DurationModeFixed},
		"source_observation": {workercontract.DurationModeFixed, workercontract.CapacityModeUnbounded, workercontract.DurationModeFixed},
	})
	positive := map[string]int64{
		"bot_webhook_inbox.max_body_bytes":             profile.BotWebhookInbox.MaxBodyBytes,
		"bot_webhook_inbox.dedup_ttl_ms":               profile.BotWebhookInbox.DedupTTLMS,
		"bot_webhook_inbox.dedup_timeout_ms":           profile.BotWebhookInbox.DedupTimeoutMS,
		"bot_webhook_inbox.poll_interval_ms":           profile.BotWebhookInbox.PollIntervalMS,
		"bot_webhook_inbox.claim_lease_ms":             profile.BotWebhookInbox.ClaimLeaseMS,
		"bot_webhook_inbox.heartbeat_interval_ms":      profile.BotWebhookInbox.HeartbeatIntervalMS,
		"bot_webhook_inbox.ownership_safety_margin_ms": profile.BotWebhookInbox.OwnershipSafetyMarginMS,
		"bot_webhook_inbox.retry_after_ms":             profile.BotWebhookInbox.RetryAfterMS,
		"bot_webhook_inbox.maintenance_interval_ms":    profile.BotWebhookInbox.MaintenanceIntervalMS,
		"bot_webhook_inbox.settlement_timeout_ms":      profile.BotWebhookInbox.SettlementTimeoutMS,
		"bot_webhook_inbox.terminal_retention_ms":      profile.BotWebhookInbox.TerminalRetentionMS,
		"bot_reply_outbox.poll_interval_ms":            profile.BotReplyOutbox.PollIntervalMS,
		"bot_reply_outbox.claim_lease_ms":              profile.BotReplyOutbox.ClaimLeaseMS,
		"bot_reply_outbox.dispatch_budget_ms":          profile.BotReplyOutbox.DispatchBudgetMS,
		"bot_reply_outbox.retry_after_ms":              profile.BotReplyOutbox.RetryAfterMS,
		"bot_reply_outbox.maintenance_interval_ms":     profile.BotReplyOutbox.MaintenanceIntervalMS,
		"bot_reply_outbox.manual_review_retention_ms":  profile.BotReplyOutbox.ManualReviewRetentionMS,
		"bot_reply_outbox.automatic_replay_horizon_ms": profile.BotReplyOutbox.AutomaticReplayHorizonMS,
		"source_observation.claim_interval_ms":         profile.SourceObservation.ClaimIntervalMS,
		"source_observation.claim_lease_ms":            profile.SourceObservation.ClaimLeaseMS,
		"source_observation.transaction_timeout_ms":    profile.SourceObservation.TransactionTimeoutMS,
		"source_observation.shutdown_timeout_ms":       profile.SourceObservation.ShutdownTimeoutMS,
	}
	problems = append(problems, positiveValueProblems(positive)...)
	problems = append(problems, apiWorkerRelationshipProblems(profile, workers)...)
	return joinWorkerProfileProblems("API", problems)
}

func apiWorkerRelationshipProblems(profile *APIWorkerProfile, workers map[string]workercontract.WorkerProfile) []string {
	problems := make([]string, 0)
	if profile.BotWebhookInbox.MaxAttempts <= 0 || profile.BotReplyOutbox.MaxAttempts <= 0 || !allPositiveInts(
		profile.SourceObservation.DBOperationConcurrency,
		profile.SourceObservation.ClaimBatchSize,
	) {
		problems = append(problems, "worker integer settings must be positive")
	}
	if profile.BotWebhookInbox.HeartbeatIntervalMS+profile.BotWebhookInbox.OwnershipSafetyMarginMS >= profile.BotWebhookInbox.ClaimLeaseMS {
		problems = append(problems, "bot_webhook_inbox heartbeat budget must fit claim lease")
	}
	if profile.BotReplyOutbox.DispatchBudgetMS >= profile.BotReplyOutbox.ClaimLeaseMS {
		problems = append(problems, "bot_reply_outbox dispatch budget must be below claim lease")
	}
	if profile.BotReplyOutbox.MaintenanceIntervalMS != profile.BotWebhookInbox.MaintenanceIntervalMS {
		problems = append(problems, "bot durable maintenance intervals must match")
	}
	if profile.SourceObservation.ClaimBatchSize < workers["source_observation"].Executor.ConfiguredWorkers ||
		profile.SourceObservation.DBOperationConcurrency < workers["source_observation"].Executor.ConfiguredWorkers {
		problems = append(problems, "source_observation batch and DB concurrency must cover workers")
	}
	return problems
}

func validateAlarmWorkerProfile(profile *AlarmWorkerProfile) error {
	if profile == nil {
		return errors.New("alarm worker profile is nil")
	}
	workers := profile.Loaded.Profile.Workers
	problems := validateWorkerShapes(workers, map[string]workerShape{
		"alarm_dispatch":        {workercontract.DurationModeFixed, workercontract.CapacityModeUnbounded, workercontract.DurationModeFixed},
		"notification_delivery": {workercontract.DurationModeFixed, workercontract.CapacityModeUnbounded, workercontract.DurationModeFixed},
		"youtube_delivery":      {workercontract.DurationModeFixed, workercontract.CapacityModeUnbounded, workercontract.DurationModeFixed},
	})
	problems = append(problems, alarmPositiveValueProblems(profile)...)
	problems = append(problems, alarmPositiveIntProblems(profile)...)
	problems = append(problems, alarmRelationshipProblems(profile, workers)...)
	return joinWorkerProfileProblems("alarm-worker", problems)
}

func alarmPositiveValueProblems(profile *AlarmWorkerProfile) []string {
	return positiveValueProblems(map[string]int64{
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

func alarmPositiveIntProblems(profile *AlarmWorkerProfile) []string {
	return positiveIntProblems(map[string]int{
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

func alarmRelationshipProblems(profile *AlarmWorkerProfile, workers map[string]workercontract.WorkerProfile) []string {
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

func validateCollectorWorkerProfile(profile *YouTubeCollectorWorkerProfile) error {
	if profile == nil {
		return errors.New("youtube collector worker profile is nil")
	}
	workers := profile.Loaded.Profile.Workers
	problems := validateWorkerShapes(workers, map[string]workerShape{
		"collection": {workercontract.DurationModePerJob, workercontract.CapacityModeBounded, workercontract.DurationModeFixed},
	})
	positive := map[string]int64{
		"collection.acquisition_cadence_ms":        profile.Collection.AcquisitionCadenceMS,
		"collection.lease_ttl_ms":                  profile.Collection.LeaseTTLMS,
		"collection.renew_interval_ms":             profile.Collection.RenewIntervalMS,
		"collection.renew_timeout_ms":              profile.Collection.RenewTimeoutMS,
		"collection.db_timeout_ms":                 profile.Collection.DBTimeoutMS,
		"collection.cleanup_timeout_ms":            profile.Collection.CleanupTimeoutMS,
		"collection.provider_admission_timeout_ms": profile.Collection.ProviderAdmissionTimeoutMS,
		"collection.collection_overhead_ms":        profile.Collection.CollectionOverheadMS,
		"collection.publish_timeout_ms":            profile.Collection.PublishTimeoutMS,
		"collection.retry_min_ms":                  profile.Collection.RetryMinMS,
		"collection.retry_max_ms":                  profile.Collection.RetryMaxMS,
		"collection.release_jitter_min_ms":         profile.Collection.ReleaseJitterMinMS,
		"collection.release_jitter_max_ms":         profile.Collection.ReleaseJitterMaxMS,
	}
	problems = append(problems, positiveValueProblems(positive)...)
	worker := workers["collection"]
	problems = append(problems, collectorCapacityProblems(profile, &worker)...)
	problems = append(problems, collectorConcurrencyProblems(profile, &worker)...)
	problems = append(problems, collectorTimingProblems(profile)...)
	return joinWorkerProfileProblems("youtube-collector", problems)
}

func collectorCapacityProblems(profile *YouTubeCollectorWorkerProfile, worker *workercontract.WorkerProfile) []string {
	capacity := int64(0)
	if worker != nil && worker.Queue.Capacity.Items != nil {
		capacity = *worker.Queue.Capacity.Items
	}
	if profile.Collection.AcquisitionBatch < 1 || int64(profile.Collection.AcquisitionBatch) > capacity {
		return []string{"collection acquisition batch must fit queue capacity"}
	}
	return nil
}

func collectorConcurrencyProblems(profile *YouTubeCollectorWorkerProfile, worker *workercontract.WorkerProfile) []string {
	problems := make([]string, 0)
	if worker == nil {
		return []string{"collection worker is missing"}
	}
	for name, value := range map[string]int{
		"holodex_max_inflight":   profile.Collection.HolodexMaxInflight,
		"official_max_inflight":  profile.Collection.OfficialMaxInflight,
		"youtubejs_max_inflight": profile.Collection.YouTubeJSMaxInflight,
	} {
		if value < 1 || value > worker.Executor.ConfiguredWorkers {
			problems = append(problems, "collection "+name+" must be within configured workers")
		}
	}
	return problems
}

func collectorTimingProblems(profile *YouTubeCollectorWorkerProfile) []string {
	problems := make([]string, 0)
	if profile.Collection.RenewIntervalMS+profile.Collection.RenewTimeoutMS+1000 >= profile.Collection.LeaseTTLMS {
		problems = append(problems, "collection renewal budget must fit lease TTL")
	}
	if profile.Collection.RetryMaxMS < profile.Collection.RetryMinMS || profile.Collection.ReleaseJitterMaxMS < profile.Collection.ReleaseJitterMinMS {
		problems = append(problems, "collection retry or jitter range is invalid")
	}
	return problems
}
