package durability

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	durabilityTestLease = 30 * time.Second

	testDurableRoomID = "room-1"
	testClaimToken    = "token-a"
	testMessageID     = "message:m-1"
	testOperatorEmail = "operator@example.com"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := OpenStore(t.Context(), StoreOptions(filepath.Join(t.TempDir(), "durable", "bot-durable.db"), nil))
	require.NoError(t, err)

	t.Cleanup(func() {
		assert.NoError(t, store.Close(context.WithoutCancel(t.Context())))
	})

	return store
}

func truncateDurabilityTables(ctx context.Context, t *testing.T, store *Store) {
	t.Helper()

	_, err := store.Exec(ctx, `DELETE FROM bot_webhook_heads;
DELETE FROM bot_webhook_inbox;
DELETE FROM bot_command_executions;
DELETE FROM bot_reply_outbox;
DELETE FROM bot_webhook_nonces;
DELETE FROM sqlite_sequence;`)
	require.NoError(t, err)
}

func inboxRow(ctx context.Context, t *testing.T, store *Store, messageID string) (status string, payload []byte, attempts int32, claimToken *string) {
	t.Helper()

	err := store.QueryRow(ctx,
		"SELECT status, payload, attempts, claim_token FROM bot_webhook_inbox WHERE message_id = ?1",
		messageID,
	).Scan(&status, &payload, &attempts, &claimToken)
	require.NoError(t, err)

	return status, payload, attempts, claimToken
}

func TestInboxRepository(t *testing.T) {
	store := newTestStore(t)
	repo := NewInboxRepository(store)
	ctx := t.Context()

	message := InboxMessage{
		MessageID:   testMessageID,
		RoomID:      testDurableRoomID,
		OrderingKey: "room:room-1",
		Payload:     []byte(`{"body":"first"}`),
	}

	t.Run("admit is keyed by message id and never overwrites the payload", func(t *testing.T) {
		assertInboxAdmitIsKeyedByMessageID(ctx, t, store, repo, message)
	})

	t.Run("claim leases exactly one due row and counts the attempt", func(t *testing.T) {
		assertInboxClaimLeasesOneDueRow(ctx, t, store, repo, message)
	})

	t.Run("heartbeat returns the authoritative renewed lease deadline", func(t *testing.T) {
		assertInboxHeartbeatRenewsLease(ctx, t, store, repo, message)
	})

	t.Run("complete is fenced by the claim token", func(t *testing.T) {
		assertInboxCompleteIsFenced(ctx, t, store, repo, message)
	})

	t.Run("release is fenced and defers the next attempt", func(t *testing.T) {
		assertInboxReleaseIsFencedAndDefers(ctx, t, store, repo, message)
	})

	t.Run("claim returns no row on an empty inbox", func(t *testing.T) {
		assertInboxClaimReturnsNoRowOnEmptyInbox(ctx, t, store, repo)
	})

	t.Run("invalid arguments are rejected before touching the store", func(t *testing.T) {
		assertInboxRejectsInvalidArguments(ctx, t, repo, message)
	})
}

func assertInboxAdmitIsKeyedByMessageID(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *InboxRepository,
	message InboxMessage,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	admission, err := repo.AdmitResult(ctx, message)
	require.NoError(t, err)
	assert.Equal(t, workercontract.AdmissionAccepted, admission)

	redelivered := message

	redelivered.Payload = []byte(`{"body":"second"}`)
	admission, err = repo.AdmitResult(ctx, redelivered)
	require.NoError(t, err, "중복 webhook 재전송은 실패가 아니다")
	assert.NotEqual(t, workercontract.AdmissionAccepted, admission)

	_, payload, _, _ := inboxRow(ctx, t, store, message.MessageID)
	assert.JSONEq(t, `{"body":"first"}`, string(payload))
}

func assertInboxClaimLeasesOneDueRow(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *InboxRepository,
	message InboxMessage,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)
	admitOne(ctx, t, repo, message)

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)
	assert.Equal(t, message.MessageID, claim.MessageID)
	assert.Equal(t, message.OrderingKey, claim.OrderingKey)
	assert.Equal(t, int32(1), claim.Attempts)

	var storedLeaseUntil int64

	require.NoError(t, store.QueryRow(ctx,
		"SELECT lease_until FROM bot_webhook_inbox WHERE message_id = ?1", message.MessageID).Scan(&storedLeaseUntil))
	assert.Equal(t, storedLeaseUntil, claim.LeaseUntil.UnixMilli())

	second, err := repo.Claim(ctx, "token-b", durabilityTestLease)
	require.NoError(t, err)
	assert.Nil(t, second, "leased row must not be claimable again")

	status, _, _, token := inboxRow(ctx, t, store, message.MessageID)
	assert.Equal(t, "processing", status)
	require.NotNil(t, token)
	assert.Equal(t, testClaimToken, *token)
}

func assertInboxHeartbeatRenewsLease(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *InboxRepository,
	message InboxMessage,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)
	admitOne(ctx, t, repo, message)

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)

	renewedUntil, applied, err := repo.Heartbeat(ctx, message.MessageID, testClaimToken, time.Minute)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Greater(t, renewedUntil, claim.LeaseUntil)

	var storedLeaseUntil int64

	require.NoError(t, store.QueryRow(ctx,
		"SELECT lease_until FROM bot_webhook_inbox WHERE message_id = ?1", message.MessageID).Scan(&storedLeaseUntil))
	assert.Equal(t, storedLeaseUntil, renewedUntil.UnixMilli())
}

func assertInboxCompleteIsFenced(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *InboxRepository,
	message InboxMessage,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)
	admitOne(ctx, t, repo, message)

	_, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)

	applied, err := repo.Complete(ctx, message.MessageID, "token-stale")
	require.NoError(t, err)
	assert.False(t, applied, "stale token must transition zero rows")

	applied, err = repo.Complete(ctx, message.MessageID, testClaimToken)
	require.NoError(t, err)
	assert.True(t, applied)

	applied, err = repo.Complete(ctx, message.MessageID, testClaimToken)
	require.NoError(t, err)
	assert.False(t, applied, "settled row must not transition twice")

	status, _, _, token := inboxRow(ctx, t, store, message.MessageID)
	assert.Equal(t, "succeeded", status)
	assert.Nil(t, token)
}

func assertInboxReleaseIsFencedAndDefers(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *InboxRepository,
	message InboxMessage,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)
	admitOne(ctx, t, repo, message)

	_, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)

	outcome, err := repo.Release(ctx, message.MessageID, "token-stale", 1, 3, time.Minute, "boom")
	require.NoError(t, err)
	assert.Equal(t, InboxReleaseNotOwned, outcome)

	outcome, err = repo.Release(ctx, message.MessageID, testClaimToken, 1, 3, time.Minute, "boom")
	require.NoError(t, err)
	assert.Equal(t, InboxReleaseRetried, outcome)

	status, _, attempts, token := inboxRow(ctx, t, store, message.MessageID)
	assert.Equal(t, "retry", status)
	assert.Equal(t, int32(1), attempts)
	assert.Nil(t, token)

	claim, err := repo.Claim(ctx, "token-c", durabilityTestLease)
	require.NoError(t, err)
	assert.Nil(t, claim, "released row must stay invisible until available_at")
}

func assertInboxClaimReturnsNoRowOnEmptyInbox(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *InboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	assert.Nil(t, claim)
}

func assertInboxRejectsInvalidArguments(
	ctx context.Context,
	t *testing.T,
	repo *InboxRepository,
	message InboxMessage,
) {
	t.Helper()

	_, err := repo.AdmitResult(ctx, InboxMessage{RoomID: testDurableRoomID, OrderingKey: "k", Payload: []byte(`{}`)})
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = repo.Claim(ctx, testClaimToken, 0)
	require.ErrorIs(t, err, ErrInvalidArgument)

	_, err = repo.Complete(ctx, message.MessageID, "  ")
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestInboxRepositoryWithoutPool(t *testing.T) {
	repo := NewInboxRepository(nil)

	_, err := repo.AdmitResult(t.Context(), InboxMessage{
		MessageID:   testMessageID,
		RoomID:      testDurableRoomID,
		OrderingKey: "room:room-1",
		Payload:     []byte(`{}`),
	})
	require.ErrorIs(t, err, ErrStoreNotConfigured)
}

func admitOne(ctx context.Context, t *testing.T, repo *InboxRepository, msg InboxMessage) {
	t.Helper()

	admission, err := repo.AdmitResult(ctx, msg)
	require.NoError(t, err)
	require.Equal(t, workercontract.AdmissionAccepted, admission)
}
