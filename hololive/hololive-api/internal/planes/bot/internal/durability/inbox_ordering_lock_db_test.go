package durability

import (
	"testing"

	"github.com/park285/shared-go/postgres/pgxdb"
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
		if rollbackErr := pgxdb.Rollback(ctx, owner); rollbackErr != nil {
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
		if rollbackErr := pgxdb.Rollback(ctx, observer); rollbackErr != nil {
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
