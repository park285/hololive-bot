package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFailurePolicyUsesOnlyOwnerAttemptBudget(t *testing.T) {
	t.Parallel()

	policy, err := NewRetryPolicy(3, time.Minute)
	require.NoError(t, err)

	now := time.Date(2026, time.August, 31, 12, 0, 0, 999, time.FixedZone("KST", 9*60*60))
	owner := testRow(1, StatusSending, 1, now.Add(-time.Hour))
	follower := testRow(2, StatusPending, 8, now.Add(-time.Minute))

	decision, err := EvaluateFailure(policy, owner, []RowSnapshot{follower}, FailureRetryable, 2*time.Minute)
	require.NoError(t, err)

	retry, ok := decision.(RetryLogicalGroupDecision)
	require.True(t, ok)
	require.Equal(t, RuleRetryScheduled, retry.RuleID())

	mutations := retry.Mutations()
	require.Len(t, mutations, 2)
	require.Equal(t, 2, mutations[0].NextAttempt())
	require.Equal(t, 8, mutations[1].NextAttempt(), "follower must not spend an independent attempt")
	require.Equal(t, DelayedDue(2*time.Minute, time.Time{}), mutations[0].Due(), "retry-after above backoff must win")
	require.Equal(t, mutations[0].Due(), mutations[1].Due())
}

func TestFailurePolicyExhaustsOwnerAndMirrorsFollower(t *testing.T) {
	t.Parallel()

	policy, err := NewRetryPolicy(2, time.Minute)
	require.NoError(t, err)

	now := time.Date(2026, time.August, 31, 3, 0, 0, 0, time.UTC)

	decision, err := EvaluateFailure(
		policy,
		testRow(1, StatusPending, 1, now.Add(-time.Hour)),
		[]RowSnapshot{testRow(2, StatusPending, 0, now.Add(-time.Minute))},
		FailureRetryable,
		0,
	)
	require.NoError(t, err)

	failed, ok := decision.(FailLogicalGroupDecision)
	require.True(t, ok)
	require.Equal(t, RuleRetryExhausted, failed.RuleID())

	mutations := failed.Mutations()
	if len(mutations) != 2 {
		t.Fatalf("mutation count = %d, want 2", len(mutations))
	}

	require.Equal(t, StatusFailed, mutations[0].NextStatus())
	require.Equal(t, StatusFailed, mutations[1].NextStatus())
	require.Equal(t, 0, mutations[1].NextAttempt())
	require.Equal(t, DueKeep, mutations[0].Due().Mode, "terminal failure keeps next_attempt_at")
	require.Equal(t, DueKeep, mutations[1].Due().Mode, "terminal failure keeps next_attempt_at")
}

func TestOutcomeUnknownProducesNoStateDecision(t *testing.T) {
	t.Parallel()

	policy, err := NewRetryPolicy(2, time.Minute)
	require.NoError(t, err)

	now := time.Date(2026, time.August, 31, 3, 0, 0, 0, time.UTC)
	decision, err := EvaluateFailure(policy, testRow(1, StatusSending, 0, now), nil, FailureOutcomeUnknown, 0)
	require.Error(t, err)
	require.Nil(t, decision)
}

func TestReviveResetsLogicalOwnerAndFollowers(t *testing.T) {
	t.Parallel()

	policy, err := NewRevivePolicy(true, 2*time.Hour, 10)
	require.NoError(t, err)

	now := time.Date(2026, time.August, 31, 3, 0, 0, 0, time.UTC)
	decision, err := EvaluateRevive(policy, ReviveInput{
		Owner:            testRow(1, StatusFailed, 3, now.Add(-time.Hour)),
		Followers:        []RowSnapshot{testRow(2, StatusFailed, 7, now.Add(-30*time.Minute))},
		OutboxNeverSent:  true,
		SourceObservedAt: now.Add(-time.Minute),
	}, now)
	require.NoError(t, err)
	require.Equal(t, RuleLogicalGroupRevived, decision.RuleID())

	for _, mutation := range decision.Mutations() {
		require.Equal(t, StatusPending, mutation.NextStatus())
		require.Zero(t, mutation.NextAttempt())
		require.Equal(t, DelayedDue(0, time.Time{}), mutation.Due())
	}
}

func TestReviveRejectsLogicalLedgerEvidence(t *testing.T) {
	t.Parallel()

	policy, err := NewRevivePolicy(true, time.Hour, 10)
	require.NoError(t, err)

	now := time.Date(2026, time.August, 31, 3, 0, 0, 0, time.UTC)

	_, err = EvaluateRevive(policy, ReviveInput{
		Owner:            testRow(1, StatusFailed, 3, now.Add(-time.Hour)),
		LedgerPresent:    true,
		OutboxNeverSent:  true,
		SourceObservedAt: now,
	}, now)
	require.ErrorContains(t, err, "ledger evidence")
}

func testRow(id int64, status DeliveryStatus, attempt int, createdAt time.Time) RowSnapshot {
	return RowSnapshot{
		DeliveryID: id, Status: status, AttemptCount: attempt,
		NextAttemptAt: createdAt.Add(time.Minute), LockedAt: createdAt.Add(2 * time.Minute),
		RowVersion: int64(attempt + 1), CreatedAt: createdAt,
	}
}
