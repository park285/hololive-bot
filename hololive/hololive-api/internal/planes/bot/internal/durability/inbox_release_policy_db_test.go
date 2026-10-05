package durability

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInboxReleaseRejectsStaleAttemptWithoutAdvancingHead(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	ctx := t.Context()
	first := inboxMessage("message:attempt-first", "room", "room:attempt-policy")
	second := inboxMessage("message:attempt-second", "room", first.OrderingKey)
	admitOne(ctx, t, repo, first)
	admitOne(ctx, t, repo, second)

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)

	outcome, err := repo.Release(ctx, first.MessageID, testClaimToken, claim.Attempts+1, 1, time.Millisecond, "failure")
	require.NoError(t, err)
	require.Equal(t, InboxReleaseNotOwned, outcome)

	status, _, attempts, token := inboxRow(ctx, t, pool, first.MessageID)
	require.Equal(t, "processing", status)
	require.Equal(t, claim.Attempts, attempts)
	require.NotNil(t, token)

	blocked, err := repo.Claim(ctx, "blocked-token", durabilityTestLease)
	require.NoError(t, err)
	require.Nil(t, blocked)

	outcome, err = repo.Release(ctx, first.MessageID, testClaimToken, claim.Attempts, 1, time.Millisecond, "failure")
	require.NoError(t, err)
	require.Equal(t, InboxReleaseAbandoned, outcome)

	next, err := repo.Claim(ctx, "next-token", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, next)
	require.Equal(t, second.MessageID, next.MessageID)
}
