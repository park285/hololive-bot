package scraping

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type ChannelSourceHealth struct {
	ChannelID           string        `json:"channel_id"`
	Source              FailureSource `json:"source"`
	ConsecutiveFailures int           `json:"consecutive_failures"`
	LastFailureReason   FailureReason `json:"last_failure_reason"`
	LastFailureAt       time.Time     `json:"last_failure_at"`
	LastSuccessAt       time.Time     `json:"last_success_at"`
	NextAllowedAt       time.Time     `json:"next_allowed_at"`
}

type ChannelHealthPolicy struct {
	Enforce           bool
	TTL               time.Duration
	ParserDriftBase   time.Duration
	ParserDriftMax    time.Duration
	TransportBase     time.Duration
	TransportMax      time.Duration
	TimeoutBase       time.Duration
	TimeoutMax        time.Duration
	HTTPStatusBase    time.Duration
	HTTPStatusMax     time.Duration
	SuccessDecaySteps int
}

func DefaultChannelHealthPolicy() ChannelHealthPolicy {
	return ChannelHealthPolicy{
		Enforce:           false,
		TTL:               24 * time.Hour,
		ParserDriftBase:   10 * time.Minute,
		ParserDriftMax:    6 * time.Hour,
		TransportBase:     2 * time.Minute,
		TransportMax:      30 * time.Minute,
		TimeoutBase:       2 * time.Minute,
		TimeoutMax:        30 * time.Minute,
		HTTPStatusBase:    5 * time.Minute,
		HTTPStatusMax:     time.Hour,
		SuccessDecaySteps: 1,
	}
}

type ChannelHealthStore struct {
	store  stateStore
	policy ChannelHealthPolicy
}

func NewChannelHealthStore(store stateStore, policy ChannelHealthPolicy) *ChannelHealthStore {
	policy = normalizeChannelHealthPolicy(policy)
	return &ChannelHealthStore{store: store, policy: policy}
}

func normalizeChannelHealthPolicy(policy ChannelHealthPolicy) ChannelHealthPolicy {
	defaults := DefaultChannelHealthPolicy()
	fillPolicyDuration(&policy.TTL, defaults.TTL)
	fillPolicyDuration(&policy.ParserDriftBase, defaults.ParserDriftBase)
	fillPolicyDuration(&policy.ParserDriftMax, defaults.ParserDriftMax)
	fillPolicyDuration(&policy.TransportBase, defaults.TransportBase)
	fillPolicyDuration(&policy.TransportMax, defaults.TransportMax)
	fillPolicyDuration(&policy.TimeoutBase, defaults.TimeoutBase)
	fillPolicyDuration(&policy.TimeoutMax, defaults.TimeoutMax)
	fillPolicyDuration(&policy.HTTPStatusBase, defaults.HTTPStatusBase)
	fillPolicyDuration(&policy.HTTPStatusMax, defaults.HTTPStatusMax)
	if policy.SuccessDecaySteps <= 0 {
		policy.SuccessDecaySteps = defaults.SuccessDecaySteps
	}
	return policy
}

func fillPolicyDuration(value *time.Duration, fallback time.Duration) {
	if value != nil && *value <= 0 {
		*value = fallback
	}
}

func (s *ChannelHealthStore) ShouldSkip(ctx context.Context, channelID string, source FailureSource, now time.Time) (time.Duration, bool) {
	if s == nil || s.store == nil || !s.policy.Enforce {
		return 0, false
	}
	health, ok := s.Get(ctx, channelID, source)
	if !ok || health.NextAllowedAt.IsZero() {
		return 0, false
	}
	remaining := health.NextAllowedAt.Sub(now)
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}

func (s *ChannelHealthStore) RecordSuccess(ctx context.Context, channelID string, source FailureSource, now time.Time) {
	if s == nil || s.store == nil {
		return
	}
	health, _ := s.Get(ctx, channelID, source)
	health.ChannelID = strings.TrimSpace(channelID)
	health.Source = source
	health.LastSuccessAt = now
	health.NextAllowedAt = time.Time{}
	if health.ConsecutiveFailures > 0 {
		health.ConsecutiveFailures -= s.policy.SuccessDecaySteps
		if health.ConsecutiveFailures < 0 {
			health.ConsecutiveFailures = 0
		}
	}
	if health.ConsecutiveFailures == 0 {
		health.LastFailureReason = FailureReasonNone
	}
	s.persist(ctx, channelID, source, health, "success")
}

func (s *ChannelHealthStore) RecordFailure(ctx context.Context, channelID string, detail FailureDetail, now time.Time) time.Duration {
	if s == nil || s.store == nil {
		return 0
	}
	source := detail.Source
	if source == "" {
		source = FailureSourceHTML
	}
	delay := s.delayFor(detail.Reason, 1)
	if delay <= 0 {
		return 0
	}
	health, _ := s.Get(ctx, channelID, source)
	health.ChannelID = strings.TrimSpace(channelID)
	health.Source = source
	health.ConsecutiveFailures++
	health.LastFailureReason = detail.Reason
	health.LastFailureAt = now
	delay = max(detail.RetryAfter, s.delayFor(detail.Reason, health.ConsecutiveFailures))
	health.NextAllowedAt = now.Add(delay)
	s.persist(ctx, channelID, source, health, "failure")
	return delay
}

func (s *ChannelHealthStore) Get(ctx context.Context, channelID string, source FailureSource) (ChannelSourceHealth, bool) {
	var health ChannelSourceHealth
	if s == nil || s.store == nil {
		return health, false
	}
	if err := s.store.Get(ctx, channelHealthStateKey(channelID, source), &health); err != nil {
		return health, false
	}
	return health, strings.TrimSpace(health.ChannelID) != ""
}

func (s *ChannelHealthStore) persist(ctx context.Context, channelID string, source FailureSource, health ChannelSourceHealth, operation string) {
	if err := s.store.Set(ctx, channelHealthStateKey(channelID, source), health, s.policy.TTL); err != nil {
		slog.Warn("failed to persist youtube producer channel health",
			"operation", operation,
			"channel_id", channelID,
			"source", source,
			"error", err)
	}
}

func (s *ChannelHealthStore) delayFor(reason FailureReason, failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	base, maxDelay, ok := s.delayBounds(reason)
	if !ok {
		return 0
	}
	delay := base
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	return delay
}

func (s *ChannelHealthStore) delayBounds(reason FailureReason) (time.Duration, time.Duration, bool) {
	bounds := map[FailureReason][2]time.Duration{
		FailureReasonParserDrift: {s.policy.ParserDriftBase, s.policy.ParserDriftMax},
		FailureReasonTransport:   {s.policy.TransportBase, s.policy.TransportMax},
		FailureReasonTimeout:     {s.policy.TimeoutBase, s.policy.TimeoutMax},
		FailureReasonHTTPStatus:  {s.policy.HTTPStatusBase, s.policy.HTTPStatusMax},
	}
	selected, ok := bounds[reason]
	if !ok {
		return 0, 0, false
	}
	return selected[0], selected[1], true
}

func channelHealthStateKey(channelID string, source FailureSource) string {
	return fmt.Sprintf("youtube:producer:channel-health:%s:%s", strings.TrimSpace(string(source)), strings.TrimSpace(channelID))
}
