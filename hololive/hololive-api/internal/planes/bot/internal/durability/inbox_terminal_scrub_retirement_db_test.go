package durability

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const retiredScrubPayload = `{"body":"retained"}`

// 현재 네 writer가 terminal 전이에서 직접 비우고 retry에서는 원본을 보존한다.
func TestInboxCompleteScrubsWithoutCompatibilityTrigger(t *testing.T) {
	pool, repo := claimedInboxForScrubRetirement(t)

	applied, err := repo.Complete(t.Context(), testMessageID, testClaimToken)
	require.NoError(t, err)
	require.True(t, applied)

	assertScrubRetirementInbox(t, pool, "succeeded", `{}`)
}

func TestInboxAbandonScrubsWithoutCompatibilityTrigger(t *testing.T) {
	pool, repo := claimedInboxForScrubRetirement(t)

	applied, err := repo.Abandon(t.Context(), testMessageID, testClaimToken, "invalid message")
	require.NoError(t, err)
	require.True(t, applied)

	assertScrubRetirementInbox(t, pool, "dead", `{}`)
}

func TestInboxReleasePreservesRetryAndScrubsTerminalWithoutCompatibilityTrigger(t *testing.T) {
	t.Run("retry retains payload", func(t *testing.T) {
		pool, repo := claimedInboxForScrubRetirement(t)

		outcome, err := repo.Release(t.Context(), testMessageID, testClaimToken, 1, 2, time.Second, "retryable")
		require.NoError(t, err)
		require.Equal(t, InboxReleaseRetried, outcome)

		assertScrubRetirementInbox(t, pool, "retry", retiredScrubPayload)
	})

	t.Run("exhausted attempts scrub payload", func(t *testing.T) {
		pool, repo := claimedInboxForScrubRetirement(t)

		outcome, err := repo.Release(t.Context(), testMessageID, testClaimToken, 1, 1, time.Second, "exhausted")
		require.NoError(t, err)
		require.Equal(t, InboxReleaseAbandoned, outcome)

		assertScrubRetirementInbox(t, pool, "dead", `{}`)
	})
}

func TestInboxReclaimPreservesRetryAndScrubsTerminalWithoutCompatibilityTrigger(t *testing.T) {
	t.Run("retry retains payload", func(t *testing.T) {
		pool, repo := claimedInboxForScrubRetirement(t)
		expireScrubRetirementLease(t, pool)

		reclaim, err := repo.ReclaimExpired(t.Context(), 2, 1)
		require.NoError(t, err)
		require.Equal(t, InboxReclaim{Requeued: 1}, reclaim)

		assertScrubRetirementInbox(t, pool, "retry", retiredScrubPayload)
	})

	t.Run("exhausted attempts scrub payload", func(t *testing.T) {
		pool, repo := claimedInboxForScrubRetirement(t)
		expireScrubRetirementLease(t, pool)

		reclaim, err := repo.ReclaimExpired(t.Context(), 1, 1)
		require.NoError(t, err)
		require.Equal(t, InboxReclaim{Abandoned: 1}, reclaim)

		assertScrubRetirementInbox(t, pool, "dead", `{}`)
	})
}

func claimedInboxForScrubRetirement(t *testing.T) (*pgxpool.Pool, *InboxRepository) {
	t.Helper()

	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	message := InboxMessage{
		MessageID:   testMessageID,
		RoomID:      testDurableRoomID,
		OrderingKey: "room:room-1",
		Payload:     []byte(retiredScrubPayload),
	}

	admitted, err := repo.Admit(t.Context(), message)
	require.NoError(t, err)
	require.True(t, admitted)

	claim, err := repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, testMessageID, claim.MessageID)

	return pool, repo
}

func expireScrubRetirementLease(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `UPDATE bot_webhook_inbox
		SET lease_until = clock_timestamp() - interval '1 second'
		WHERE message_id = $1`, testMessageID)
	require.NoError(t, err)
}

func assertScrubRetirementInbox(t *testing.T, pool *pgxpool.Pool, wantStatus, wantPayload string) {
	t.Helper()

	status, payload, _, token := inboxRow(t.Context(), t, pool, testMessageID)
	require.Equal(t, wantStatus, status)
	require.JSONEq(t, wantPayload, string(payload))
	require.Nil(t, token)
}
