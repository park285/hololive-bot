package dbx

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestDeleteOneBatchStopsAtBatchSize(t *testing.T) {
	ctx := t.Context()
	pool := newBatchDeleteTestPool(t, 5)

	deleted, err := DeleteOneBatch(ctx, pool, batchDeleteTestSpec(2))
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	require.EqualValues(t, 3, countBatchDeleteTestRows(ctx, t, pool))
}

func TestDeleteInBatchesDeletesEveryMatchingRow(t *testing.T) {
	ctx := t.Context()
	pool := newBatchDeleteTestPool(t, 5)

	deleted, err := DeleteInBatches(ctx, pool, batchDeleteTestSpec(2))
	require.NoError(t, err)
	require.EqualValues(t, 5, deleted)
	require.EqualValues(t, 0, countBatchDeleteTestRows(ctx, t, pool))
}

func TestDeleteInBatchesRejectsNonPositiveBatchSize(t *testing.T) {
	ctx := t.Context()
	pool := newBatchDeleteTestPool(t, 1)

	deleted, err := DeleteInBatches(ctx, pool, batchDeleteTestSpec(0))
	require.Error(t, err)
	require.Zero(t, deleted)
	require.EqualValues(t, 1, countBatchDeleteTestRows(ctx, t, pool))
}

func TestDeleteInBatchesStopsOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	pool := newBatchDeleteTestPool(t, 4)

	cancel()

	deleted, err := DeleteInBatches(ctx, pool, batchDeleteTestSpec(2))
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, deleted)
	require.EqualValues(t, 4, countBatchDeleteTestRows(t.Context(), t, pool))
}

func TestDeleteInBatchesRejectsNilQuerier(t *testing.T) {
	deleted, err := DeleteInBatches(t.Context(), nil, batchDeleteTestSpec(2))
	require.Error(t, err)
	require.Zero(t, deleted)
}

func batchDeleteTestSpec(batchSize int) BatchDeleteSpec {
	return BatchDeleteSpec{
		Query: `DELETE FROM dbx_batch_delete_test
		        WHERE ctid IN (SELECT ctid FROM dbx_batch_delete_test WHERE value = $1 LIMIT $2)`,
		Args:      []any{"stale"},
		BatchSize: batchSize,
		Yield:     time.Millisecond,
	}
}

func newBatchDeleteTestPool(t *testing.T, rows int) *pgxpool.Pool {
	t.Helper()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(ctx, `
		CREATE TABLE dbx_batch_delete_test (
			id BIGSERIAL PRIMARY KEY,
			value TEXT NOT NULL
		)
	`)
	require.NoError(t, err)

	for range rows {
		_, err = pool.Exec(ctx, "INSERT INTO dbx_batch_delete_test (value) VALUES ($1)", "stale")
		require.NoError(t, err)
	}

	return pool
}

func countBatchDeleteTestRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var count int64

	require.NoError(t, pool.QueryRow(ctx, "SELECT count(id) FROM dbx_batch_delete_test").Scan(&count))

	return count
}
