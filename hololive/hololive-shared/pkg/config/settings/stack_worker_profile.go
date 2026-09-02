package settings

import (
	"errors"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

func LoadAPIWorkerProfile() (*APIWorkerProfile, error) {
	loaded, err := workercontract.LoadProfileFromEnv("hololive", "api")
	if err != nil {
		return nil, fmt.Errorf("load stack worker profile: %w", err)
	}

	profile := &APIWorkerProfile{Loaded: loaded}
	if err := workercontract.DecodeWorkerSettings(loaded, "bot_webhook_inbox", &profile.BotWebhookInbox); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := workercontract.DecodeWorkerSettings(loaded, "bot_reply_outbox", &profile.BotReplyOutbox); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := workercontract.DecodeWorkerSettings(loaded, "source_observation", &profile.SourceObservation); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := validateAPIWorkerProfile(profile); err != nil {
		return nil, fmt.Errorf("validate API worker profile: %w", err)
	}

	return profile, nil
}

func validateAPIWorkerProfile(profile *APIWorkerProfile) error {
	if profile == nil {
		return errors.New("API worker profile is nil")
	}

	workers := profile.Loaded.Profile.Workers
	problems := workercontract.ShapeProblems(workers, map[string]workercontract.WorkerShape{
		"bot_webhook_inbox":  {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeFixed},
		"bot_reply_outbox":   {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeFixed},
		"source_observation": {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeFixed},
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

	problems = append(problems, load.PositiveValueProblems(positive)...)
	problems = append(problems, apiWorkerRelationshipProblems(profile, workers)...)

	if err := load.JoinWorkerProfileProblems("API", problems); err != nil {
		return fmt.Errorf("join worker profile problems: %w", err)
	}

	return nil
}

func apiWorkerRelationshipProblems(profile *APIWorkerProfile, workers map[string]workercontract.WorkerProfile) []string {
	problems := make([]string, 0)

	if profile.BotWebhookInbox.MaxAttempts <= 0 || profile.BotReplyOutbox.MaxAttempts <= 0 || !load.AllPositiveInts(
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
