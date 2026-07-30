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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-dbtest"
)

const durabilityTestLease = 30 * time.Second

func newDurabilityPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	return dbtest.NewPool(t)
}

func truncateDurabilityTables(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(ctx,
		"TRUNCATE bot_webhook_inbox, bot_command_executions, bot_reply_outbox RESTART IDENTITY")
	require.NoError(t, err)
}

func inboxRow(ctx context.Context, t *testing.T, pool *pgxpool.Pool, messageID string) (status string, payload []byte, attempts int32, claimToken *string) {
	t.Helper()

	err := pool.QueryRow(ctx,
		"SELECT status, payload, attempts, claim_token FROM bot_webhook_inbox WHERE message_id = $1",
		messageID,
	).Scan(&status, &payload, &attempts, &claimToken)
	require.NoError(t, err)

	return status, payload, attempts, claimToken
}

func TestInboxRepository(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewInboxRepository(pool)
	ctx := context.Background()

	message := InboxMessage{
		MessageID:   "message:m-1",
		RoomID:      "room-1",
		OrderingKey: "room:room-1",
		Payload:     []byte(`{"body":"first"}`),
	}

	t.Run("admit is keyed by message id and never overwrites the payload", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)

		admitted, err := repo.Admit(ctx, message)
		require.NoError(t, err)
		assert.True(t, admitted)

		redelivered := message
		redelivered.Payload = []byte(`{"body":"second"}`)
		admitted, err = repo.Admit(ctx, redelivered)
		require.NoError(t, err, "중복 webhook 재전송은 실패가 아니다")
		assert.False(t, admitted)

		_, payload, _, _ := inboxRow(ctx, t, pool, message.MessageID)
		assert.JSONEq(t, `{"body":"first"}`, string(payload))
	})

	t.Run("claim leases exactly one due row and counts the attempt", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		admitOne(ctx, t, repo, message)

		claim, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)
		require.NotNil(t, claim)
		assert.Equal(t, message.MessageID, claim.MessageID)
		assert.Equal(t, message.OrderingKey, claim.OrderingKey)
		assert.Equal(t, int32(1), claim.Attempts)

		second, err := repo.Claim(ctx, "token-b", durabilityTestLease)
		require.NoError(t, err)
		assert.Nil(t, second, "leased row must not be claimable again")

		status, _, _, token := inboxRow(ctx, t, pool, message.MessageID)
		assert.Equal(t, "processing", status)
		require.NotNil(t, token)
		assert.Equal(t, "token-a", *token)
	})

	t.Run("complete is fenced by the claim token", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		admitOne(ctx, t, repo, message)
		_, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)

		applied, err := repo.Complete(ctx, message.MessageID, "token-stale")
		require.NoError(t, err)
		assert.False(t, applied, "stale token must transition zero rows")

		applied, err = repo.Complete(ctx, message.MessageID, "token-a")
		require.NoError(t, err)
		assert.True(t, applied)

		applied, err = repo.Complete(ctx, message.MessageID, "token-a")
		require.NoError(t, err)
		assert.False(t, applied, "settled row must not transition twice")

		status, _, _, token := inboxRow(ctx, t, pool, message.MessageID)
		assert.Equal(t, "succeeded", status)
		assert.Nil(t, token)
	})

	t.Run("release is fenced and defers the next attempt", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)
		admitOne(ctx, t, repo, message)
		_, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)

		applied, err := repo.Release(ctx, message.MessageID, "token-stale", time.Minute, "boom")
		require.NoError(t, err)
		assert.False(t, applied)

		applied, err = repo.Release(ctx, message.MessageID, "token-a", time.Minute, "boom")
		require.NoError(t, err)
		assert.True(t, applied)

		status, _, attempts, token := inboxRow(ctx, t, pool, message.MessageID)
		assert.Equal(t, "retry", status)
		assert.Equal(t, int32(1), attempts)
		assert.Nil(t, token)

		claim, err := repo.Claim(ctx, "token-c", durabilityTestLease)
		require.NoError(t, err)
		assert.Nil(t, claim, "released row must stay invisible until available_at")
	})

	t.Run("claim returns no row on an empty inbox", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)

		claim, err := repo.Claim(ctx, "token-a", durabilityTestLease)
		require.NoError(t, err)
		assert.Nil(t, claim)
	})

	t.Run("invalid arguments are rejected before touching postgres", func(t *testing.T) {
		_, err := repo.Admit(ctx, InboxMessage{RoomID: "room-1", OrderingKey: "k", Payload: []byte(`{}`)})
		require.ErrorIs(t, err, ErrInvalidArgument)

		_, err = repo.Claim(ctx, "token-a", 0)
		require.ErrorIs(t, err, ErrInvalidArgument)

		_, err = repo.Complete(ctx, message.MessageID, "  ")
		require.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestInboxRepositoryWithoutPool(t *testing.T) {
	repo := NewInboxRepository(nil)

	_, err := repo.Admit(context.Background(), InboxMessage{
		MessageID:   "message:m-1",
		RoomID:      "room-1",
		OrderingKey: "room:room-1",
		Payload:     []byte(`{}`),
	})
	require.ErrorIs(t, err, ErrPoolNotConfigured)
}

func admitOne(ctx context.Context, t *testing.T, repo *InboxRepository, msg InboxMessage) {
	t.Helper()

	admitted, err := repo.Admit(ctx, msg)
	require.NoError(t, err)
	require.True(t, admitted)
}
