package durability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
	"github.com/park285/iris-client-go/v6/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
)

func newReplyOutboxEntry(identity string, ordinal uint64, payload string) *ReplyOutboxEntry {
	return &ReplyOutboxEntry{
		MessageID:       identity,
		Phase:           transport.ReplyPhase,
		Ordinal:         ordinal,
		RoomID:          testDurableRoomID,
		Payload:         []byte(payload),
		ClientRequestID: transport.ReplyClientRequestID(identity, ordinal),
	}
}

func replyOutboxRow(ctx context.Context, t *testing.T, store *Store, clientRequestID string) (status string, payload []byte, hash string, claimToken *string) {
	t.Helper()

	err := store.QueryRow(ctx,
		"SELECT status, payload, payload_hash, claim_token FROM bot_reply_outbox WHERE client_request_id = ?1",
		clientRequestID,
	).Scan(&status, &payload, &hash, &claimToken)
	require.NoError(t, err)

	return status, payload, hash, claimToken
}

func TestReplyOutboxRepositoryInsert(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()

	t.Run("payload is immutable for a message phase ordinal key", func(t *testing.T) {
		assertReplyOutboxPayloadIsImmutable(ctx, t, store, repo)
	})

	t.Run("ordinal separates emissions of one inbound message", func(t *testing.T) {
		assertReplyOutboxOrdinalSeparatesEmissions(ctx, t, store, repo)
	})

	t.Run("a client request id is never reused for another row", func(t *testing.T) {
		assertReplyOutboxClientRequestIDIsNeverReused(ctx, t, store, repo)
	})

	t.Run("insert trigger failures redact the message and request ids", func(t *testing.T) {
		assertReplyOutboxInsertFailureRedactsIdentity(ctx, t, store, repo)
	})

	t.Run("insert cancellation is preserved without exposing identity", func(t *testing.T) {
		assertReplyOutboxConflictInspectionRedactsIdentity(ctx, t, repo)
	})

	t.Run("phase 1a client request ids satisfy the stored iris contract", func(t *testing.T) {
		assertReplyOutboxClientRequestIDMatchesTheIrisContract(ctx, t, store, repo)
	})

	t.Run("invalid entries are rejected before touching the store", func(t *testing.T) {
		assertReplyOutboxRejectsInvalidEntries(ctx, t, repo)
	})
}

func assertReplyOutboxPayloadIsImmutable(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	entry := newReplyOutboxEntry(testMessageID, 0, `{"body":"first"}`)

	outcome, err := repo.Insert(ctx, entry)
	require.NoError(t, err)
	assert.Equal(t, ReplyOutboxInserted, outcome)

	replayed := *entry

	replayed.Payload = []byte(`{"body":"rewritten"}`)
	outcome, err = repo.Insert(ctx, &replayed)
	require.NoError(t, err)
	assert.Equal(t, ReplyOutboxPayloadDiverged, outcome)

	_, payload, hash, _ := replyOutboxRow(ctx, t, store, entry.ClientRequestID)
	assert.JSONEq(t, `{"body":"first"}`, string(payload))

	digest := sha256.Sum256(entry.Payload)
	assert.Equal(t, hex.EncodeToString(digest[:]), hash)
}

func assertReplyOutboxOrdinalSeparatesEmissions(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	for ordinal := range uint64(3) {
		outcome, err := repo.Insert(ctx, newReplyOutboxEntry(testMessageID, ordinal, `{"body":"x"}`))
		require.NoError(t, err)
		assert.Equal(t, ReplyOutboxInserted, outcome)
	}

	var count int

	require.NoError(t, store.QueryRow(ctx,
		"SELECT count(id) FROM bot_reply_outbox WHERE message_id = ?1", testMessageID,
	).Scan(&count))
	assert.Equal(t, 3, count)
}

func assertReplyOutboxClientRequestIDIsNeverReused(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	entry := newReplyOutboxEntry(testMessageID, 0, `{"body":"x"}`)
	outcome, err := repo.Insert(ctx, entry)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxInserted, outcome)

	const rawID = "message:raw-private-outbox-id"

	colliding := newReplyOutboxEntry(rawID, 0, `{"body":"y"}`)

	colliding.ClientRequestID = entry.ClientRequestID
	_, err = repo.Insert(ctx, colliding)
	require.Error(t, err, "409 계약을 지키려면 같은 client_request_id가 다른 payload를 가리킬 수 없다")
	assert.Contains(t, err.Error(), "insert reply outbox row")
	assert.NotContains(t, err.Error(), rawID)
	assert.NotContains(t, err.Error(), entry.ClientRequestID)
	assert.Contains(t, err.Error(), "message_token=anon:")
	assert.Contains(t, err.Error(), "reason=database_operation_failed")

	sqliteErr, ok := errors.AsType[*sqlite3.Error](err)
	if !ok {
		t.Fatalf("error type = %T, want *sqlite3.Error in chain", err)
	}

	assert.Equal(t, sqlite3.CONSTRAINT_UNIQUE, sqliteErr.ExtendedCode())
	assert.Contains(t, sqliteErr.Error(), "bot_reply_outbox.client_request_id")
}

func assertReplyOutboxInsertFailureRedactsIdentity(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	const rawID = "message:raw-private-trigger-id"

	entry := newReplyOutboxEntry(rawID, 0, `{"body":"x"}`)
	_, err := store.Exec(ctx, `CREATE TRIGGER fail_reply_insert_for_privacy_test
		BEFORE INSERT ON bot_reply_outbox
		BEGIN
			SELECT RAISE(ABORT, 'database rejected ' || NEW.message_id || ' ' || NEW.client_request_id);
		END`)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelCleanup()

		_, cleanupErr := store.Exec(cleanupCtx, "DROP TRIGGER IF EXISTS fail_reply_insert_for_privacy_test")
		require.NoError(t, cleanupErr)
	})

	_, err = repo.Insert(ctx, entry)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), rawID)
	assert.NotContains(t, err.Error(), entry.ClientRequestID)
	assert.Contains(t, err.Error(), "message_token=anon:")
	assert.Contains(t, err.Error(), "reason=database_operation_failed")

	sqliteErr, ok := errors.AsType[*sqlite3.Error](err)
	require.True(t, ok, "error type = %T, want *sqlite3.Error in chain", err)
	assert.Contains(t, sqliteErr.Error(), rawID)
	assert.Contains(t, sqliteErr.Error(), entry.ClientRequestID)
}

func assertReplyOutboxConflictInspectionRedactsIdentity(
	ctx context.Context,
	t *testing.T,
	repo *ReplyOutboxRepository,
) {
	t.Helper()

	const rawID = "message:raw-private-conflict-id"

	entry := newReplyOutboxEntry(rawID, 0, `{"body":"x"}`)
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	_, err := repo.Insert(canceled, entry)
	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), rawID)
	assert.NotContains(t, err.Error(), entry.ClientRequestID)
	assert.Contains(t, err.Error(), "message_token=anon:")
	assert.Contains(t, err.Error(), "reason=context_canceled")
}

func assertReplyOutboxClientRequestIDMatchesTheIrisContract(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	canonical := newReplyOutboxEntry(testMessageID, 0, `{"body":"x"}`)
	require.Equal(t, "hololive:v1:message:m-1:reply:0", canonical.ClientRequestID)

	outcome, err := repo.Insert(ctx, canonical)
	require.NoError(t, err)
	assert.Equal(t, ReplyOutboxInserted, outcome)

	hashed := newReplyOutboxEntry("message:닉네임 with/slash", 7, `{"body":"x"}`)
	require.NotContains(t, hashed.ClientRequestID, "/")

	outcome, err = repo.Insert(ctx, hashed)
	require.NoError(t, err, "해시 폴백 id도 CHECK 정규식을 통과해야 한다")
	assert.Equal(t, ReplyOutboxInserted, outcome)

	notice := newReplyOutboxEntry("schedule-notice:9:18478615493603057", 0, `{"body":"x"}`)

	outcome, err = repo.Insert(ctx, notice)
	require.NoError(t, err, "수신 메시지 없이 보내는 일정 알림 키도 저장된다")
	assert.Equal(t, ReplyOutboxInserted, outcome)
}

func assertReplyOutboxRejectsInvalidEntries(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository) {
	t.Helper()

	entry := newReplyOutboxEntry(testMessageID, 0, `{"body":"x"}`)

	entry.Payload = nil

	_, err := repo.Insert(ctx, entry)
	require.ErrorIs(t, err, ErrInvalidArgument)

	entry = newReplyOutboxEntry(testMessageID, 0, `{"body":"x"}`)
	entry.ClientRequestID = "   "
	_, err = repo.Insert(ctx, entry)
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestReplyOutboxRepositoryTransitions(t *testing.T) {
	store := newTestStore(t)
	repo := newTestReplyOutboxRepository(store)
	ctx := t.Context()

	entry := newReplyOutboxEntry(testMessageID, 0, `{"body":"x"}`)

	t.Run("accept is fenced by the claim token", func(t *testing.T) {
		clockStore := newTestStore(t)
		assertReplyOutboxAcceptIsFenced(t.Context(), t, clockStore, newTestReplyOutboxRepository(clockStore), entry)
	})

	t.Run("conflict settles the row terminally and releases the lease", func(t *testing.T) {
		assertReplyOutboxConflictSettlesTerminally(ctx, t, store, repo, entry)
	})

	t.Run("retryable settlement returns the row to the claim queue", func(t *testing.T) {
		assertReplyOutboxRetryableSettlementRequeues(ctx, t, store, repo, entry)
	})

	t.Run("settle rejects statuses outside the ledger vocabulary", func(t *testing.T) {
		_, err := repo.Settle(ctx, ReplyOutboxSettlement{ID: 1, ClaimToken: testClaimToken, Status: "submitting"})
		require.ErrorIs(t, err, ErrInvalidArgument)
	})

	t.Run("claim returns no row on an empty outbox", func(t *testing.T) {
		assertReplyOutboxClaimReturnsNoRowOnEmptyOutbox(ctx, t, store, repo)
	})
}

func assertReplyOutboxAcceptIsFenced(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
	entry *ReplyOutboxEntry,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	claim := claimOne(ctx, t, repo, entry)

	base := time.Now()

	var clockReads atomic.Int64

	store.now = func() time.Time { return base.Add(time.Duration(clockReads.Add(1)) * time.Millisecond) }

	applied, err := repo.SettleAccepted(ctx, claim.ID, "token-stale", "iris-1", time.Second)
	require.NoError(t, err)
	assert.False(t, applied, "stale token must transition zero rows")

	applied, err = repo.SettleAccepted(ctx, claim.ID, testClaimToken, "iris-1", time.Second)
	require.NoError(t, err)
	assert.True(t, applied)

	var (
		status, irisRequestID   string
		leaseReleased           bool
		accepted, next, updated int64
	)

	require.NoError(t, store.QueryRow(ctx, `SELECT status, iris_request_id,
			claim_token IS NULL AND lease_until IS NULL AND accepted_at IS NOT NULL,
			accepted_at, handoff_next_check_at, updated_at
		FROM bot_reply_outbox WHERE id = ?1`, claim.ID).Scan(&status, &irisRequestID, &leaseReleased, &accepted, &next, &updated))
	assert.Equal(t, ReplyOutboxAccepted, status)
	assert.Equal(t, "iris-1", irisRequestID)
	assert.True(t, leaseReleased, "accepted는 발송 lease 없이 handoff 확인을 기다린다")
	assert.Equal(t, time.Second.Milliseconds(), next-accepted, "첫 handoff 확인은 admission 정산 시각 기준이다")
	assert.Equal(t, accepted, updated)
}

func assertReplyOutboxConflictSettlesTerminally(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
	entry *ReplyOutboxEntry,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	claim := claimOne(ctx, t, repo, entry)

	applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
		ID:         claim.ID,
		ClaimToken: "token-stale",
		Status:     ReplyOutboxPermanentConflict,
		LastError:  "409",
	})
	require.NoError(t, err)
	assert.False(t, applied)

	settlement := ReplyOutboxSettlement{
		ID:         claim.ID,
		ClaimToken: testClaimToken,
		Status:     ReplyOutboxPermanentConflict,
		LastError:  "409 client request id reused with a different payload",
	}

	applied, err = repo.Settle(ctx, settlement)
	require.NoError(t, err)
	assert.True(t, applied)

	applied, err = repo.Settle(ctx, settlement)
	require.NoError(t, err)
	assert.False(t, applied, "terminal row must not settle twice")

	status, _, _, token := replyOutboxRow(ctx, t, store, entry.ClientRequestID)
	assert.Equal(t, ReplyOutboxPermanentConflict, status)
	assert.Nil(t, token)
}

func assertReplyOutboxRetryableSettlementRequeues(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
	entry *ReplyOutboxEntry,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	claim := claimOne(ctx, t, repo, entry)

	applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
		ID:         claim.ID,
		ClaimToken: testClaimToken,
		Status:     ReplyOutboxRetryablePreDispatch,
		LastError:  "transport reset",
		RetryAfter: time.Millisecond,
	})
	require.NoError(t, err)
	require.True(t, applied)
	advanceReplyRetry(ctx, t, store, claim.ID)

	reclaimed, err := repo.Claim(ctx, "token-b", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, reclaimed)
	assert.Equal(t, int32(2), reclaimed.Attempts)
	assert.Equal(t, entry.ClientRequestID, reclaimed.ClientRequestID)
}

func assertReplyOutboxClaimReturnsNoRowOnEmptyOutbox(
	ctx context.Context,
	t *testing.T,
	store *Store,
	repo *ReplyOutboxRepository,
) {
	t.Helper()
	truncateDurabilityTables(ctx, t, store)

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	assert.Nil(t, claim)
}

func TestReplyOutboxRepositoryWithoutPool(t *testing.T) {
	repo := newTestReplyOutboxRepository(nil)

	_, err := repo.Insert(t.Context(), newReplyOutboxEntry(testMessageID, 0, `{"body":"x"}`))
	require.ErrorIs(t, err, ErrStoreNotConfigured)
}

func TestReplyOutboxClientRequestIDMatchesTransportDerivation(t *testing.T) {
	t.Parallel()

	const identity = testMessageID

	for ordinal := range uint64(4) {
		entry := newReplyOutboxEntry(identity, ordinal, `{"body":"x"}`)
		assert.Equal(t, transport.ReplyClientRequestID(identity, ordinal), entry.ClientRequestID)
		assert.True(t, strings.HasPrefix(entry.ClientRequestID, "hololive:v1:"+identity+":reply:"))
	}
}

func claimOne(ctx context.Context, t *testing.T, repo *ReplyOutboxRepository, entry *ReplyOutboxEntry) *ReplyOutboxClaim {
	t.Helper()

	outcome, err := repo.Insert(ctx, entry)
	require.NoError(t, err)
	require.Equal(t, ReplyOutboxInserted, outcome)

	claim, err := repo.Claim(ctx, testClaimToken, durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)

	return claim
}

func advanceReplyRetry(ctx context.Context, t *testing.T, store *Store, id int64) {
	t.Helper()

	_, err := store.Exec(ctx, `UPDATE bot_reply_outbox SET row_version = row_version + 1, available_at = available_at - 1 WHERE id = ?1`, id)
	require.NoError(t, err)
}

func makeReplyRetryDue(ctx context.Context, store *Store, id int64, delay time.Duration) error {
	result, err := store.Exec(ctx, `UPDATE bot_reply_outbox SET row_version = row_version + 1, available_at = ?3
		WHERE id = ?1 AND available_at - updated_at = ?2`, id, delay.Milliseconds(), time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("advance reply outbox retry: %w", err)
	}

	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return fmt.Errorf("outbox row %d did not store retry delay %s", id, delay)
	}

	return nil
}

func newTestReplyOutboxRepository(store *Store) *ReplyOutboxRepository {
	return &ReplyOutboxRepository{store: store, maxAttempts: 5, automaticReplayHorizon: iris.ReplyAutomaticReplayHorizon}
}
