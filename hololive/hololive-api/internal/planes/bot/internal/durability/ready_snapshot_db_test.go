package durability

import (
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

func TestInboxReadySnapshotPreservesHeadAndScheduledAge(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)

	for _, id := range []string{"message:head", "message:follower"} {
		admitOne(t.Context(), t, repo, InboxMessage{MessageID: id, RoomID: testDurableRoomID, OrderingKey: "room:one", Payload: []byte(`{"body":"ready"}`)})
	}

	_, err := pool.Exec(t.Context(), `UPDATE bot_webhook_inbox SET available_at = clock_timestamp() + INTERVAL '1 hour' WHERE message_id = 'message:head'`)
	require.NoError(t, err)

	snapshot, err := repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.Depth)

	claim, err := repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.Nil(t, claim)

	_, err = pool.Exec(t.Context(), `UPDATE bot_webhook_inbox SET created_at = clock_timestamp() - INTERVAL '1 day', available_at = clock_timestamp() WHERE message_id = 'message:head'`)
	require.NoError(t, err)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), snapshot.Depth)
	require.Less(t, snapshot.OldestAge, 5*time.Second)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), tx)) }()

	_, err = tx.Exec(t.Context(), `SELECT id FROM bot_webhook_inbox WHERE message_id = 'message:head' FOR UPDATE`)
	require.NoError(t, err)

	claim, err = repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.Nil(t, claim, "locked head cannot expose its follower")
	require.NoError(t, tx.Rollback(t.Context()))

	claim, err = repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, "message:head", claim.MessageID)

	_, err = pool.Exec(t.Context(), `UPDATE bot_webhook_inbox SET lease_until = clock_timestamp() - INTERVAL '1 second' WHERE message_id = 'message:head'`)
	require.NoError(t, err)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.Depth, "expired processing needs maintenance reclaim first")
}

func TestReplyReadySnapshotPreservesPredecessorSafetyAndScheduledAge(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := newTestReplyOutboxRepository(pool)
	first := newReplyOutboxEntry("message:head", 0, `{"body":"head"}`)
	second := newReplyOutboxEntry("message:follower", 0, `{"body":"follower"}`)

	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t.Context(), t, repo, first))
	require.Equal(t, ReplyOutboxInserted, mustInsertReply(t.Context(), t, repo, second))

	_, err := pool.Exec(t.Context(), `UPDATE bot_reply_outbox SET available_at = clock_timestamp() + INTERVAL '1 hour' WHERE client_request_id = $1`, first.ClientRequestID)
	require.NoError(t, err)

	snapshot, err := repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.Depth)

	_, err = pool.Exec(t.Context(), `UPDATE bot_reply_outbox SET created_at = clock_timestamp() - INTERVAL '1 day', available_at = clock_timestamp() WHERE client_request_id = $1`, first.ClientRequestID)
	require.NoError(t, err)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), snapshot.Depth)
	require.Less(t, snapshot.OldestAge, 5*time.Second)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), tx)) }()

	_, err = tx.Exec(t.Context(), `SELECT id FROM bot_reply_outbox WHERE client_request_id = $1 FOR UPDATE`, first.ClientRequestID)
	require.NoError(t, err)

	claim, err := repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.Nil(t, claim, "SKIP LOCKED cannot bypass a room predecessor")
	require.NoError(t, tx.Rollback(t.Context()))

	_, err = pool.Exec(t.Context(), `UPDATE bot_reply_outbox SET attempts = $2 WHERE client_request_id = $1`, first.ClientRequestID, repo.maxAttempts)
	require.NoError(t, err)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Zero(t, snapshot.Depth, "expired head is maintenance work and still blocks the successor")

	claim, err = repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.Nil(t, claim)

	status, _, _, _ := replyOutboxRow(t.Context(), t, pool, first.ClientRequestID)
	require.Equal(t, ReplyOutboxManualReview, status)

	snapshot, err = repo.ReadySnapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(1), snapshot.Depth)

	claim, err = repo.Claim(t.Context(), testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, second.ClientRequestID, claim.ClientRequestID)
}
