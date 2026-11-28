package durability

import (
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"
)

const retiredScrubPayload = `{"body":"retained"}`

func TestInboxCompleteScrubsWithoutCompatibilityTrigger(t *testing.T) {
	store, repo := claimedInboxForScrubRetirement(t)

	applied, err := repo.Complete(t.Context(), testMessageID, testClaimToken)
	require.NoError(t, err)
	require.True(t, applied)

	assertScrubRetirementInbox(t, store, "succeeded", `{}`)
}

func TestInboxAbandonScrubsWithoutCompatibilityTrigger(t *testing.T) {
	store, repo := claimedInboxForScrubRetirement(t)

	applied, err := repo.Abandon(t.Context(), testMessageID, testClaimToken, "invalid message")
	require.NoError(t, err)
	require.True(t, applied)

	assertScrubRetirementInbox(t, store, "dead", `{}`)
}

func TestInboxReleasePreservesRetryAndScrubsTerminalWithoutCompatibilityTrigger(t *testing.T) {
	t.Run("retry retains payload", func(t *testing.T) {
		store, repo := claimedInboxForScrubRetirement(t)

		outcome, err := repo.Release(t.Context(), testMessageID, testClaimToken, 1, 2, time.Second, "retryable")
		require.NoError(t, err)
		require.Equal(t, InboxReleaseRetried, outcome)

		assertScrubRetirementInbox(t, store, "retry", retiredScrubPayload)
	})

	t.Run("exhausted attempts scrub payload", func(t *testing.T) {
		store, repo := claimedInboxForScrubRetirement(t)

		outcome, err := repo.Release(t.Context(), testMessageID, testClaimToken, 1, 1, time.Second, "exhausted")
		require.NoError(t, err)
		require.Equal(t, InboxReleaseAbandoned, outcome)

		assertScrubRetirementInbox(t, store, "dead", `{}`)
	})
}

func TestInboxReclaimPreservesRetryAndScrubsTerminalWithoutCompatibilityTrigger(t *testing.T) {
	t.Run("retry retains payload", func(t *testing.T) {
		store, repo := claimedInboxForScrubRetirement(t)
		expireScrubRetirementLease(t, store)

		reclaim, err := repo.ReclaimExpired(t.Context(), 2, 1)
		require.NoError(t, err)
		require.Equal(t, InboxReclaim{Requeued: 1}, reclaim)

		assertScrubRetirementInbox(t, store, "retry", retiredScrubPayload)
	})

	t.Run("exhausted attempts scrub payload", func(t *testing.T) {
		store, repo := claimedInboxForScrubRetirement(t)
		expireScrubRetirementLease(t, store)

		reclaim, err := repo.ReclaimExpired(t.Context(), 1, 1)
		require.NoError(t, err)
		require.Equal(t, InboxReclaim{Abandoned: 1}, reclaim)

		assertScrubRetirementInbox(t, store, "dead", `{}`)
	})
}

func claimedInboxForScrubRetirement(t *testing.T) (*Store, *InboxRepository) {
	t.Helper()

	store := newTestStore(t)
	repo := NewInboxRepository(store)
	message := InboxMessage{
		MessageID:   testMessageID,
		RoomID:      testDurableRoomID,
		OrderingKey: "room:room-1",
		Payload:     []byte(retiredScrubPayload),
	}

	admission, err := repo.AdmitResult(t.Context(), message)
	require.NoError(t, err)
	require.Equal(t, workercontract.AdmissionAccepted, admission)

	claim, err := repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, testMessageID, claim.MessageID)

	return store, repo
}

func expireScrubRetirementLease(t *testing.T, store *Store) {
	t.Helper()

	_, err := store.Exec(t.Context(), `UPDATE bot_webhook_inbox
		SET lease_until = ?2
		WHERE message_id = ?1`, testMessageID, time.Now().Add(-time.Second).UnixMilli())
	require.NoError(t, err)
}

func assertScrubRetirementInbox(t *testing.T, store *Store, wantStatus, wantPayload string) {
	t.Helper()

	status, payload, _, token := inboxRow(t.Context(), t, store, testMessageID)
	require.Equal(t, wantStatus, status)
	require.JSONEq(t, wantPayload, string(payload))
	require.Nil(t, token)
}
