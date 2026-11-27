package durability

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInboxReadySnapshotPreservesHeadAndScheduledAge(t *testing.T) {
	store := newTestStore(t)
	repo := NewInboxRepository(store)

	for _, id := range []string{"message:head", "message:follower"} {
		admitOne(t.Context(), t, repo, InboxMessage{MessageID: id, RoomID: testDurableRoomID, OrderingKey: "room:one", Payload: []byte(`{"body":"ready"}`)})
	}

	_, err := store.Exec(t.Context(), `UPDATE bot_webhook_inbox SET available_at = ?1 WHERE message_id = 'message:head'`, time.Now().Add(time.Hour).UnixMilli())
	require.NoError(t, err)

	snapshot, err := repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.Depth)

	claim, err := repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.Nil(t, claim, "a scheduled head cannot expose its follower")

	_, err = store.Exec(t.Context(), `UPDATE bot_webhook_inbox SET created_at = ?1, available_at = ?2 WHERE message_id = 'message:head'`,
		time.Now().Add(-24*time.Hour).UnixMilli(), time.Now().UnixMilli())
	require.NoError(t, err)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), snapshot.Depth)
	require.Less(t, snapshot.OldestAge, 5*time.Second)

	claim, err = repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, "message:head", claim.MessageID)

	_, err = store.Exec(t.Context(), `UPDATE bot_webhook_inbox SET lease_until = ?1 WHERE message_id = 'message:head'`, time.Now().Add(-time.Second).UnixMilli())
	require.NoError(t, err)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.Depth, "expired processing needs maintenance reclaim first")
}

func TestReplyReadySnapshotPreservesPredecessorSafetyAndScheduledAge(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	first := newReplyOutboxEntry("message:head", 0, `{"body":"head"}`)
	second := newReplyOutboxEntry("message:follower", 0, `{"body":"follower"}`)

	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t.Context(), t, repo, first))
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t.Context(), t, repo, second))

	_, err := store.Exec(t.Context(), `UPDATE bot_reply_outbox SET row_version = row_version + 1, available_at = ?2 WHERE client_request_id = ?1`,
		first.ClientRequestID, time.Now().Add(time.Hour).UnixMilli())
	require.NoError(t, err)

	snapshot, err := repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.Depth)

	claim, err := repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.Nil(t, claim, "a scheduled room predecessor blocks its successor")

	_, err = store.Exec(t.Context(), `UPDATE bot_reply_outbox SET row_version = row_version + 1, created_at = ?2, available_at = ?3 WHERE client_request_id = ?1`,
		first.ClientRequestID, time.Now().Add(-24*time.Hour).UnixMilli(), time.Now().UnixMilli())
	require.NoError(t, err)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), snapshot.Depth)
	require.Less(t, snapshot.OldestAge, 5*time.Second)

	_, err = store.Exec(t.Context(), `UPDATE bot_reply_outbox SET row_version = row_version + 1, attempts = ?2 WHERE client_request_id = ?1`, first.ClientRequestID, repo.maxAttempts)
	require.NoError(t, err)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.Depth, "expired head is maintenance work and still blocks the successor")

	claim, err = repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.Nil(t, claim)

	status, _, _, _ := replyOutboxRow(t.Context(), t, store, first.ClientRequestID)
	require.Equal(t, ReplyOutboxManualReview, status)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), snapshot.Depth)

	claim, err = repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, second.ClientRequestID, claim.ClientRequestID)
}
