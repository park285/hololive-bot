// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package durability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
)

func newReplyOutboxEntry(identity string, ordinal uint64, payload string) *ReplyOutboxEntry {
	return &ReplyOutboxEntry{
		MessageID:       identity,
		Phase:           transport.ReplyPhase,
		Ordinal:         ordinal,
		RoomID:          "room-1",
		Payload:         []byte(payload),
		ClientRequestID: transport.ReplyClientRequestID(identity, ordinal),
	}
}

func replyOutboxRow(ctx context.Context, t *testing.T, pool *pgxpool.Pool, clientRequestID string) (status string, payload []byte, hash string, claimToken *string) {
	t.Helper()

	err := pool.QueryRow(ctx,
		"SELECT status, payload, payload_hash, claim_token FROM bot_reply_outbox WHERE client_request_id = $1",
		clientRequestID,
	).Scan(&status, &payload, &hash, &claimToken)
	require.NoError(t, err)

	return status, payload, hash, claimToken
}

func TestReplyOutboxRepositoryInsert(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()

	t.Run("payload is immutable for a message phase ordinal key", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-1", 0, `{"body":"first"}`)

		outcome, err := repo.Insert(ctx, entry)
		require.NoError(t, err)
		assert.Equal(t, ReplyOutboxInserted, outcome)

		replayed := *entry
		replayed.Payload = []byte(`{"body":"rewritten"}`)
		outcome, err = repo.Insert(ctx, &replayed)
		require.NoError(t, err)
		assert.Equal(t, ReplyOutboxPayloadDiverged, outcome)

		_, payload, hash, _ := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
		assert.JSONEq(t, `{"body":"first"}`, string(payload))
		digest := sha256.Sum256(entry.Payload)
		assert.Equal(t, hex.EncodeToString(digest[:]), hash)
	})

	t.Run("ordinal separates emissions of one inbound message", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)

		for ordinal := range uint64(3) {
			outcome, err := repo.Insert(ctx, newReplyOutboxEntry("message:m-1", ordinal, `{"body":"x"}`))
			require.NoError(t, err)
			assert.Equal(t, ReplyOutboxInserted, outcome)
		}

		var count int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT count(id) FROM bot_reply_outbox WHERE message_id = $1", "message:m-1",
		).Scan(&count))
		assert.Equal(t, 3, count)
	})

	t.Run("a client request id is never reused for another row", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		entry := newReplyOutboxEntry("message:m-1", 0, `{"body":"x"}`)
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
		var pgErr *pgconn.PgError
		require.True(t, errors.As(err, &pgErr), "typed database cause must remain available to errors.As")
		assert.Equal(t, "bot_reply_outbox_client_request_id_key", pgErr.ConstraintName)
	})

	t.Run("insert trigger failures redact the message and request ids", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		const rawID = "message:raw-private-trigger-id"
		entry := newReplyOutboxEntry(rawID, 0, `{"body":"x"}`)
		_, err := pool.Exec(ctx, `
			CREATE OR REPLACE FUNCTION fail_reply_insert_for_privacy_test() RETURNS trigger AS $$
			BEGIN RAISE EXCEPTION 'database rejected % %', NEW.message_id, NEW.client_request_id; END
			$$ LANGUAGE plpgsql;
			DROP TRIGGER IF EXISTS fail_reply_insert_for_privacy_test ON bot_reply_outbox;
			CREATE TRIGGER fail_reply_insert_for_privacy_test
			BEFORE INSERT ON bot_reply_outbox
			FOR EACH ROW EXECUTE FUNCTION fail_reply_insert_for_privacy_test()`)
		require.NoError(t, err)
		t.Cleanup(func() {
			cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancelCleanup()
			_, cleanupErr := pool.Exec(cleanupCtx, "DROP TRIGGER IF EXISTS fail_reply_insert_for_privacy_test ON bot_reply_outbox")
			require.NoError(t, cleanupErr)
		})

		_, err = repo.Insert(ctx, entry)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), rawID)
		assert.NotContains(t, err.Error(), entry.ClientRequestID)
		assert.Contains(t, err.Error(), "message_token=anon:")
		assert.Contains(t, err.Error(), "reason=database_operation_failed")
		var pgErr *pgconn.PgError
		require.True(t, errors.As(err, &pgErr))
		assert.Contains(t, pgErr.Error(), rawID)
		assert.Contains(t, pgErr.Error(), entry.ClientRequestID)
	})

	t.Run("conflict inspection preserves cancellation without exposing identity", func(t *testing.T) {
		const rawID = "message:raw-private-conflict-id"
		entry := newReplyOutboxEntry(rawID, 0, `{"body":"x"}`)
		canceled, cancel := context.WithCancel(ctx)
		cancel()

		_, err := repo.classifyRecordedPayload(canceled, entry, "payload-hash")
		require.ErrorIs(t, err, context.Canceled)
		assert.NotContains(t, err.Error(), rawID)
		assert.NotContains(t, err.Error(), entry.ClientRequestID)
		assert.Contains(t, err.Error(), "message_token=anon:")
		assert.Contains(t, err.Error(), "reason=context_canceled")
	})

	t.Run("phase 1a client request ids satisfy the stored iris contract", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)

		canonical := newReplyOutboxEntry("message:m-1", 0, `{"body":"x"}`)
		require.Equal(t, "hololive:v1:message:m-1:reply:0", canonical.ClientRequestID)
		outcome, err := repo.Insert(ctx, canonical)
		require.NoError(t, err)
		assert.Equal(t, ReplyOutboxInserted, outcome)

		hashed := newReplyOutboxEntry("message:닉네임 with/slash", 7, `{"body":"x"}`)
		require.NotContains(t, hashed.ClientRequestID, "/")
		outcome, err = repo.Insert(ctx, hashed)
		require.NoError(t, err, "해시 폴백 id도 CHECK 정규식을 통과해야 한다")
		assert.Equal(t, ReplyOutboxInserted, outcome)
	})

	t.Run("invalid entries are rejected before touching postgres", func(t *testing.T) {
		entry := newReplyOutboxEntry("message:m-1", 0, `{"body":"x"}`)
		entry.Payload = nil
		_, err := repo.Insert(ctx, entry)
		require.ErrorIs(t, err, ErrInvalidArgument)

		entry = newReplyOutboxEntry("message:m-1", 0, `{"body":"x"}`)
		entry.ClientRequestID = "   "
		_, err = repo.Insert(ctx, entry)
		require.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestReplyOutboxRepositoryTransitions(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewReplyOutboxRepository(pool)
	ctx := context.Background()

	entry := newReplyOutboxEntry("message:m-1", 0, `{"body":"x"}`)

	t.Run("accept is fenced by the claim token", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		claim := claimOne(ctx, t, repo, entry)

		applied, err := repo.MarkAccepted(ctx, claim.ID, "token-stale", "iris-1")
		require.NoError(t, err)
		assert.False(t, applied, "stale token must transition zero rows")

		applied, err = repo.MarkAccepted(ctx, claim.ID, "token-a", "iris-1")
		require.NoError(t, err)
		assert.True(t, applied)

		status, _, _, token := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
		assert.Equal(t, "accepted", status)
		require.NotNil(t, token)
	})

	t.Run("conflict settles the row terminally and releases the lease", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
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
			ClaimToken: "token-a",
			Status:     ReplyOutboxPermanentConflict,
			LastError:  "409 client request id reused with a different payload",
		}
		applied, err = repo.Settle(ctx, settlement)
		require.NoError(t, err)
		assert.True(t, applied)

		applied, err = repo.Settle(ctx, settlement)
		require.NoError(t, err)
		assert.False(t, applied, "terminal row must not settle twice")

		status, _, _, token := replyOutboxRow(ctx, t, pool, entry.ClientRequestID)
		assert.Equal(t, ReplyOutboxPermanentConflict, status)
		assert.Nil(t, token)
	})

	t.Run("retryable settlement returns the row to the claim queue", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		claim := claimOne(ctx, t, repo, entry)

		applied, err := repo.Settle(ctx, ReplyOutboxSettlement{
			ID:         claim.ID,
			ClaimToken: "token-a",
			Status:     ReplyOutboxRetryablePreDispatch,
			LastError:  "transport reset",
			RetryAfter: time.Millisecond,
		})
		require.NoError(t, err)
		require.True(t, applied)
		time.Sleep(5 * time.Millisecond)

		reclaimed, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, reclaimed)
		assert.Equal(t, int32(2), reclaimed.Attempts)
		assert.Equal(t, entry.ClientRequestID, reclaimed.ClientRequestID)
	})

	t.Run("settle rejects statuses outside the ledger vocabulary", func(t *testing.T) {
		_, err := repo.Settle(ctx, ReplyOutboxSettlement{ID: 1, ClaimToken: "token-a", Status: "submitting"})
		require.ErrorIs(t, err, ErrInvalidArgument)
	})

	t.Run("claim returns no row on an empty outbox", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)

		claim, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)
		assert.Nil(t, claim)
	})
}

func TestReplyOutboxRepositoryWithoutPool(t *testing.T) {
	repo := NewReplyOutboxRepository(nil)

	_, err := repo.Insert(context.Background(), newReplyOutboxEntry("message:m-1", 0, `{"body":"x"}`))
	require.ErrorIs(t, err, ErrPoolNotConfigured)

	_, err = repo.ReplayManualReview(context.Background(), ReplyOutboxManualReplay{
		OutboxID: 1, Actor: "operator@example.com", Reason: "ticket-1",
	})
	require.ErrorIs(t, err, ErrPoolNotConfigured)
}

func TestReplyOutboxClientRequestIDMatchesTransportDerivation(t *testing.T) {
	t.Parallel()

	const identity = "message:m-1"

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

	claim, err := repo.Claim(ctx, "token-a", durabilityTestLease)
	require.NoError(t, err)
	require.NotNil(t, claim)

	return claim
}
