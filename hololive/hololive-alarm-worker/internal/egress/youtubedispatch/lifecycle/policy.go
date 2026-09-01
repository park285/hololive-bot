package lifecycle

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

type RetryPolicy struct {
	maxRetries   int
	retryBackoff time.Duration
}

func NewRetryPolicy(maxRetries int, retryBackoff time.Duration) (RetryPolicy, error) {
	if maxRetries <= 0 {
		return RetryPolicy{}, errors.New("retry policy max retries must be positive")
	}

	if retryBackoff <= 0 {
		return RetryPolicy{}, errors.New("retry policy backoff must be positive")
	}

	return RetryPolicy{maxRetries: maxRetries, retryBackoff: retryBackoff}, nil
}

func (p RetryPolicy) MaxRetries() int             { return p.maxRetries }
func (p RetryPolicy) RetryBackoff() time.Duration { return p.retryBackoff }

type RevivePolicy struct {
	enabled         bool
	freshnessWindow time.Duration
	batchLimit      int
}

func NewRevivePolicy(enabled bool, freshnessWindow time.Duration, batchLimit int) (RevivePolicy, error) {
	if batchLimit <= 0 {
		return RevivePolicy{}, errors.New("revive policy batch limit must be positive")
	}

	if enabled && freshnessWindow <= 0 {
		return RevivePolicy{}, errors.New("enabled revive policy freshness window must be positive")
	}

	if freshnessWindow < 0 {
		return RevivePolicy{}, errors.New("revive policy freshness window is negative")
	}

	return RevivePolicy{enabled: enabled, freshnessWindow: freshnessWindow, batchLimit: batchLimit}, nil
}

func (p RevivePolicy) Enabled() bool                  { return p.enabled }
func (p RevivePolicy) FreshnessWindow() time.Duration { return p.freshnessWindow }
func (p RevivePolicy) BatchLimit() int                { return p.batchLimit }

// RowSnapshot is an immutable policy input copied from a database snapshot.
type RowSnapshot struct {
	DeliveryID    int64
	Status        DeliveryStatus
	AttemptCount  int
	NextAttemptAt time.Time
	LockedAt      time.Time
	RowVersion    int64
	CreatedAt     time.Time
}

func (r RowSnapshot) Validate() error {
	if r.DeliveryID <= 0 {
		return errors.New("row snapshot delivery id must be positive")
	}

	if !r.Status.Valid() {
		return fmt.Errorf("row snapshot status %q is invalid", r.Status)
	}

	if r.AttemptCount < 0 {
		return errors.New("row snapshot attempt count is negative")
	}

	if r.RowVersion < 0 {
		return errors.New("row snapshot version is negative")
	}

	if r.CreatedAt.IsZero() {
		return errors.New("row snapshot created at is zero")
	}

	return nil
}

// RowMutation is one exact expected-to-next transition. Its values are frozen
// so repositories cannot independently recalculate retry or follower policy.
type RowMutation struct {
	deliveryID       int64
	expectedStatus   DeliveryStatus
	expectedVersion  int64
	expectedAttempt  int
	expectedLockedAt time.Time
	nextStatus       DeliveryStatus
	nextVersion      int64
	nextAttempt      int
	nextAttemptAt    time.Time
	clearLock        bool
}

func (m RowMutation) DeliveryID() int64              { return m.deliveryID }
func (m RowMutation) ExpectedStatus() DeliveryStatus { return m.expectedStatus }
func (m RowMutation) ExpectedVersion() int64         { return m.expectedVersion }
func (m RowMutation) ExpectedAttempt() int           { return m.expectedAttempt }
func (m RowMutation) ExpectedLockedAt() time.Time    { return m.expectedLockedAt }
func (m RowMutation) NextStatus() DeliveryStatus     { return m.nextStatus }
func (m RowMutation) NextVersion() int64             { return m.nextVersion }
func (m RowMutation) NextAttempt() int               { return m.nextAttempt }
func (m RowMutation) NextAttemptAt() time.Time       { return m.nextAttemptAt }
func (m RowMutation) ClearsLock() bool               { return m.clearLock }

type Decision interface {
	RuleID() RuleID
	At() time.Time
	Mutations() []RowMutation
	decision()
}

type decisionBase struct {
	ruleID    RuleID
	at        time.Time
	mutations []RowMutation
}

func (d decisionBase) RuleID() RuleID           { return d.ruleID }
func (d decisionBase) At() time.Time            { return d.at }
func (d decisionBase) Mutations() []RowMutation { return slices.Clone(d.mutations) }
func (decisionBase) decision()                  {}

type (
	RetryLogicalGroupDecision  struct{ decisionBase }
	FailLogicalGroupDecision   struct{ decisionBase }
	ReviveLogicalGroupDecision struct{ decisionBase }
)

// EvaluateFailure spends only the deterministic owner's attempt budget.
func EvaluateFailure(
	policy RetryPolicy,
	owner RowSnapshot,
	followers []RowSnapshot,
	kind FailureKind,
	at time.Time,
	retryAfter time.Duration,
) (Decision, error) {
	if policy.maxRetries <= 0 || policy.retryBackoff <= 0 {
		return nil, errors.New("evaluate failure: invalid retry policy")
	}

	if err := validateFailureGroup(owner, followers); err != nil {
		return nil, fmt.Errorf("evaluate failure: %w", err)
	}

	if kind == FailureOutcomeUnknown {
		return nil, errors.New("evaluate failure: outcome unknown must not produce a state decision")
	}

	if kind != FailureRetryable && kind != FailurePermanent {
		return nil, errors.New("evaluate failure: failure kind is invalid")
	}

	if retryAfter < 0 {
		return nil, errors.New("evaluate failure: retry after is negative")
	}

	canonicalAt, err := CanonicalTime(at)
	if err != nil {
		return nil, fmt.Errorf("evaluate failure: at: %w", err)
	}

	nextOwnerAttempt := owner.AttemptCount + 1
	terminal := kind == FailurePermanent || nextOwnerAttempt >= policy.maxRetries
	ruleID := RuleRetryScheduled
	nextStatus := StatusPending
	due := canonicalAt.Add(max(policy.retryBackoff, retryAfter))

	if terminal {
		nextStatus = StatusFailed
		due = owner.NextAttemptAt
		ruleID = RuleRetryExhausted

		if kind == FailurePermanent {
			ruleID = RulePermanentFailure
		}
	}

	due = due.UTC().Truncate(time.Microsecond)

	mutations := make([]RowMutation, 0, len(followers)+1)

	mutations = append(mutations, failureMutation(owner, nextStatus, nextOwnerAttempt, due))

	for i := range followers {
		mutations = append(mutations, failureMutation(followers[i], nextStatus, followers[i].AttemptCount, due))
	}

	if terminal {
		return FailLogicalGroupDecision{ruleID: ruleID, at: canonicalAt, mutations: mutations}, nil
	}

	return RetryLogicalGroupDecision{ruleID: ruleID, at: canonicalAt, mutations: mutations}, nil
}

func validateFailureGroup(owner RowSnapshot, followers []RowSnapshot) error {
	if err := owner.Validate(); err != nil {
		return fmt.Errorf("owner: %w", err)
	}

	if owner.Status != StatusPending && owner.Status != StatusSending {
		return fmt.Errorf("owner status %q cannot fail", owner.Status)
	}

	seen := map[int64]struct{}{owner.DeliveryID: {}}

	for i := range followers {
		if err := followers[i].Validate(); err != nil {
			return fmt.Errorf("follower[%d]: %w", i, err)
		}

		if followers[i].Status != StatusPending {
			return fmt.Errorf("follower[%d] status %q is not mutable", i, followers[i].Status)
		}

		if _, ok := seen[followers[i].DeliveryID]; ok {
			return fmt.Errorf("duplicate delivery id %d", followers[i].DeliveryID)
		}

		seen[followers[i].DeliveryID] = struct{}{}
	}

	return nil
}

func failureMutation(row RowSnapshot, status DeliveryStatus, attempt int, due time.Time) RowMutation {
	return RowMutation{
		deliveryID: row.DeliveryID, expectedStatus: row.Status, expectedVersion: row.RowVersion,
		expectedAttempt: row.AttemptCount, expectedLockedAt: row.LockedAt,
		nextStatus: status, nextVersion: row.RowVersion + 1, nextAttempt: attempt,
		nextAttemptAt: due, clearLock: true,
	}
}

type ReviveInput struct {
	Owner            RowSnapshot
	Followers        []RowSnapshot
	LedgerPresent    bool
	OutboxNeverSent  bool
	SourceObservedAt time.Time
	HasActiveLock    bool
}

func EvaluateRevive(policy RevivePolicy, input ReviveInput, at time.Time) (ReviveLogicalGroupDecision, error) {
	canonicalAt, rows, err := validateRevive(policy, input, at)
	if err != nil {
		return ReviveLogicalGroupDecision{}, fmt.Errorf("evaluate revive: validate input: %w", err)
	}

	mutations, err := buildReviveMutations(rows, canonicalAt)
	if err != nil {
		return ReviveLogicalGroupDecision{}, fmt.Errorf("evaluate revive: build mutations: %w", err)
	}

	return ReviveLogicalGroupDecision{
		ruleID: RuleLogicalGroupRevived, at: canonicalAt, mutations: mutations,
	}, nil
}

func validateRevive(policy RevivePolicy, input ReviveInput, at time.Time) (time.Time, []RowSnapshot, error) {
	if !policy.enabled {
		return time.Time{}, nil, errors.New("evaluate revive: revive is disabled")
	}

	if policy.freshnessWindow <= 0 || policy.batchLimit <= 0 {
		return time.Time{}, nil, errors.New("evaluate revive: invalid revive policy")
	}

	if err := validateReviveEligibility(input); err != nil {
		return time.Time{}, nil, fmt.Errorf("validate revive: eligibility: %w", err)
	}

	canonicalAt, err := CanonicalTime(at)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("evaluate revive: at: %w", err)
	}

	observedAt, err := CanonicalTime(input.SourceObservedAt)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("evaluate revive: source observed at: %w", err)
	}

	if observedAt.Before(canonicalAt.Add(-policy.freshnessWindow)) || observedAt.After(canonicalAt) {
		return time.Time{}, nil, errors.New("evaluate revive: source is outside freshness window")
	}

	rows := make([]RowSnapshot, 0, len(input.Followers)+1)

	rows = append(rows, input.Owner)
	rows = append(rows, input.Followers...)

	if len(rows) > policy.batchLimit {
		return time.Time{}, nil, errors.New("evaluate revive: logical group exceeds batch limit")
	}

	return canonicalAt, rows, nil
}

func validateReviveEligibility(input ReviveInput) error {
	if err := input.Owner.Validate(); err != nil {
		return fmt.Errorf("evaluate revive: owner: %w", err)
	}

	if input.Owner.Status != StatusFailed {
		return errors.New("evaluate revive: deterministic owner is not failed")
	}

	if input.LedgerPresent {
		return errors.New("evaluate revive: logical ledger evidence exists")
	}

	if !input.OutboxNeverSent {
		return errors.New("evaluate revive: outbox has sent evidence")
	}

	if input.HasActiveLock {
		return errors.New("evaluate revive: logical group has an active lock")
	}

	return nil
}

func buildReviveMutations(rows []RowSnapshot, at time.Time) ([]RowMutation, error) {
	mutations := make([]RowMutation, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))

	for i := range rows {
		if err := rows[i].Validate(); err != nil {
			return nil, fmt.Errorf("evaluate revive: row[%d]: %w", i, err)
		}

		if rows[i].Status != StatusFailed {
			return nil, fmt.Errorf("evaluate revive: row[%d] is not failed", i)
		}

		if _, ok := seen[rows[i].DeliveryID]; ok {
			return nil, fmt.Errorf("evaluate revive: duplicate delivery id %d", rows[i].DeliveryID)
		}

		seen[rows[i].DeliveryID] = struct{}{}
		mutations = append(mutations, RowMutation{
			deliveryID: rows[i].DeliveryID, expectedStatus: rows[i].Status,
			expectedVersion: rows[i].RowVersion, expectedAttempt: rows[i].AttemptCount,
			expectedLockedAt: rows[i].LockedAt, nextStatus: StatusPending,
			nextVersion: rows[i].RowVersion + 1, nextAttempt: 0,
			nextAttemptAt: at, clearLock: true,
		})
	}

	return mutations, nil
}
