package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

// BotCommandStaleAfter는 command 소유권 heartbeat가 갱신해야 하는 고정 stale 경계다.
const BotCommandStaleAfter = 5 * time.Minute

// LoadAPIWorkerProfile은 hololive-api 역할의 Stack Worker Profile v1을 읽고 bot·source observation 설정 관계를 검증한다.
func LoadAPIWorkerProfile() (*APIWorkerProfile, error) {
	loaded, err := workercontract.LoadProfileFromEnv(APIWorkerIdentity())
	if err != nil {
		return nil, fmt.Errorf("load stack worker profile: %w", err)
	}

	profile := &APIWorkerProfile{Loaded: loaded}
	if err := workercontract.DecodeWorkerSettingsSet(loaded, map[string]any{
		WorkerBotWebhookInbox:   &profile.BotWebhookInbox,
		WorkerBotReplyOutbox:    &profile.BotReplyOutbox,
		WorkerSourceObservation: &profile.SourceObservation,
	}); err != nil {
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
		WorkerBotWebhookInbox:   {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeNone},
		WorkerBotReplyOutbox:    {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeNone},
		WorkerSourceObservation: {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeNone},
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
		"bot_reply_outbox.retry_after_ms":              profile.BotReplyOutbox.RetryAfterMS,
		"bot_reply_outbox.maintenance_interval_ms":     profile.BotReplyOutbox.MaintenanceIntervalMS,
		"bot_reply_outbox.manual_review_retention_ms":  profile.BotReplyOutbox.ManualReviewRetentionMS,
		"bot_reply_outbox.automatic_replay_horizon_ms": profile.BotReplyOutbox.AutomaticReplayHorizonMS,
		"source_observation.claim_interval_ms":         profile.SourceObservation.ClaimIntervalMS,
		"source_observation.claim_lease_ms":            profile.SourceObservation.ClaimLeaseMS,
		"source_observation.shutdown_timeout_ms":       profile.SourceObservation.ShutdownTimeoutMS,
	}

	problems = append(problems, runtimepolicy.PositiveValueProblems(positive)...)
	problems = append(problems, apiWorkerRelationshipProblems(profile, workers)...)
	problems = append(problems, sourceObservationProblems(profile.SourceObservation, workers[WorkerSourceObservation])...)

	if err := runtimepolicy.JoinWorkerProfileProblems("API", problems); err != nil {
		return fmt.Errorf("join worker profile problems: %w", err)
	}

	return nil
}

func apiWorkerRelationshipProblems(profile *APIWorkerProfile, workers map[string]workercontract.WorkerProfile) []string {
	problems := make([]string, 0)

	if profile.BotWebhookInbox.MaxAttempts <= 0 || profile.BotReplyOutbox.MaxAttempts <= 0 || !runtimepolicy.AllPositiveInts(
		profile.SourceObservation.DBOperationConcurrency,
		profile.SourceObservation.ClaimBatchSize,
	) {
		problems = append(problems, "worker integer settings must be positive")
	}

	if profile.BotWebhookInbox.HeartbeatIntervalMS >= profile.BotWebhookInbox.ClaimLeaseMS-profile.BotWebhookInbox.OwnershipSafetyMarginMS {
		problems = append(problems, "bot_webhook_inbox heartbeat budget must fit claim lease")
	}

	if profile.BotWebhookInbox.HeartbeatIntervalMS >= BotCommandStaleAfter.Milliseconds() {
		problems = append(problems, "bot_webhook_inbox heartbeat must fit command stale window")
	}

	if profile.BotWebhookInbox.TerminalRetentionMS <= irisdurable.IrisReplyAdmissionRetention.Milliseconds() {
		problems = append(problems, "bot_webhook_inbox terminal retention must exceed Iris reply admission retention")
	}

	if timeout, ok := workers[WorkerBotReplyOutbox].Executor.AttemptTimeout.FixedDuration(); ok && timeout >= workercontract.Milliseconds(profile.BotReplyOutbox.ClaimLeaseMS) {
		problems = append(problems, "bot_reply_outbox attempt timeout must be below claim lease")
	}

	if profile.BotReplyOutbox.MaintenanceIntervalMS != profile.BotWebhookInbox.MaintenanceIntervalMS {
		problems = append(problems, "bot durable maintenance intervals must match")
	}

	if profile.SourceObservation.ClaimBatchSize < workers[WorkerSourceObservation].Executor.ConfiguredWorkers ||
		profile.SourceObservation.DBOperationConcurrency < workers[WorkerSourceObservation].Executor.ConfiguredWorkers {
		problems = append(problems, "source_observation batch and DB concurrency must cover workers")
	}

	return problems
}

func sourceObservationProblems(settings SourceObservationWorkerSettings, worker workercontract.WorkerProfile) []string {
	var problems []string

	workers := worker.Executor.ConfiguredWorkers
	timeout, fixed := worker.Executor.AttemptTimeout.FixedDuration()
	timeoutMS := timeout.Milliseconds()

	if workers < 1 || workers > youtubePlaneMaxConsumerWorkers {
		problems = append(problems, "source_observation workers must be between 1 and 16")
	}

	validBatch := settings.ClaimBatchSize >= 1 && settings.ClaimBatchSize <= youtubePlaneMaxClaimBatchSize
	if !validBatch {
		problems = append(problems, "source_observation claim batch must be between 1 and 100")
	}

	validTransactionTimeout := fixed && timeout >= time.Second && timeout <= time.Minute
	if !validTransactionTimeout {
		problems = append(problems, "source_observation transaction timeout must be between 1s and 1m")
	}

	// 범위를 확인한 값만 곱해 잘못된 프로필의 정수 overflow가 lease 검증을 우회하지 못하게 한다.
	if validBatch && validTransactionTimeout {
		minimumLeaseMS := int64(settings.ClaimBatchSize)*timeoutMS + 10000
		if settings.ClaimLeaseMS < minimumLeaseMS {
			problems = append(problems, "source_observation claim lease must cover the batch transaction budget and 10s margin")
		}
	}

	if settings.ShutdownTimeoutMS/2 < timeoutMS {
		problems = append(problems, "source_observation shutdown timeout must cover transaction and claim release timeouts")
	}

	return problems
}
