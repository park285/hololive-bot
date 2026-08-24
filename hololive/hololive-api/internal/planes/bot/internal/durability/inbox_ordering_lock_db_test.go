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
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	orderingKeyAlpha = "room:alpha"
	orderingKeyBeta  = "room:beta"
)

func TestLockInboxOrderingKeysAcquiresEntireUniqueBatch(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx := t.Context()
	owner, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() {
		if rollbackErr := owner.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback owner transaction: %v", rollbackErr)
		}
	}()

	require.NoError(t, lockInboxOrderingKeys(ctx, owner, []string{
		orderingKeyBeta,
		orderingKeyAlpha,
		orderingKeyAlpha,
	}))

	observer, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() {
		if rollbackErr := observer.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback observer transaction: %v", rollbackErr)
		}
	}()

	for _, key := range []string{orderingKeyAlpha, orderingKeyBeta} {
		var acquired bool

		require.NoError(t, observer.QueryRow(ctx,
			"SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))", key).Scan(&acquired))
		assert.False(t, acquired, "batch owner must hold %q", key)
	}

	require.NoError(t, owner.Commit(ctx))

	for _, key := range []string{orderingKeyAlpha, orderingKeyBeta} {
		var acquired bool

		require.NoError(t, observer.QueryRow(ctx,
			"SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))", key).Scan(&acquired))
		assert.True(t, acquired, "released batch lock %q must be acquirable", key)
	}

	require.NoError(t, observer.Commit(ctx))
}

func TestLockInboxOrderingKeysSkipsEmptyBatch(t *testing.T) {
	assert.NoError(t, lockInboxOrderingKeys(t.Context(), nil, nil))
}

func TestLockInboxOrderingKeysRejectsNilTransactionForNonEmptyBatch(t *testing.T) {
	err := lockInboxOrderingKeys(t.Context(), nil, []string{orderingKeyAlpha})
	require.ErrorIs(t, err, ErrInvalidArgument)
	assert.ErrorContains(t, err, "transaction must not be nil")
}
