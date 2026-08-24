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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandExecutionRepository(t *testing.T) {
	pool := newDurabilityPool(t)
	repo := NewCommandExecutionRepository(pool)
	ctx := t.Context()

	const messageID = testMessageID

	t.Run("only the first claim for a message id wins", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)

		claimed, err := repo.Claim(ctx, messageID, "broadcast_history", testClaimToken)
		require.NoError(t, err)
		assert.True(t, claimed)

		claimed, err = repo.Claim(ctx, messageID, "broadcast_history", testClaimToken)
		require.NoError(t, err)
		assert.False(t, claimed, "재처리 시 두 번째 claim은 0 rows여야 한다")

		var count int

		require.NoError(t, pool.QueryRow(ctx,
			"SELECT count(message_id) FROM bot_command_executions WHERE message_id = $1", messageID,
		).Scan(&count))
		assert.Equal(t, 1, count)

		state, err := repo.State(ctx, messageID)
		require.NoError(t, err)
		require.NotNil(t, state)
		assert.Equal(t, CommandExecutionClaimed, state.Status)
		assert.False(t, state.ClaimedAt.IsZero())
	})

	t.Run("complete transitions a claimed execution exactly once", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)

		claimed, err := repo.Claim(ctx, messageID, "broadcast_history", testClaimToken)
		require.NoError(t, err)
		require.True(t, claimed)

		applied, err := repo.Complete(ctx, messageID, testClaimToken, CommandExecutionSucceeded)
		require.NoError(t, err)
		assert.True(t, applied)

		applied, err = repo.Complete(ctx, messageID, testClaimToken, CommandExecutionFailed)
		require.NoError(t, err)
		assert.False(t, applied, "terminal execution must not transition twice")

		var status, summary string

		require.NoError(t, pool.QueryRow(ctx,
			"SELECT status, result_summary FROM bot_command_executions WHERE message_id = $1", messageID,
		).Scan(&status, &summary))
		assert.Equal(t, CommandExecutionSucceeded, status)
		assert.Equal(t, CommandExecutionSucceeded, summary)
	})

	t.Run("complete rejects statuses outside the ledger vocabulary", func(t *testing.T) {
		truncateDurabilityTables(ctx, t, pool)

		_, err := repo.Complete(ctx, messageID, testClaimToken, "claimed")
		require.ErrorIs(t, err, ErrInvalidArgument)
	})

	t.Run("blank message id is rejected before touching postgres", func(t *testing.T) {
		_, err := repo.Claim(ctx, "  ", "broadcast_history", testClaimToken)
		require.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestCommandExecutionRepositoryWithoutPool(t *testing.T) {
	repo := NewCommandExecutionRepository(nil)

	_, err := repo.Claim(t.Context(), testMessageID, "broadcast_history", testClaimToken)
	require.ErrorIs(t, err, ErrPoolNotConfigured)

	_, err = repo.State(t.Context(), testMessageID)
	require.ErrorIs(t, err, ErrPoolNotConfigured)
}
