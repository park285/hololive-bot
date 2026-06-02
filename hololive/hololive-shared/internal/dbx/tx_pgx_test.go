package dbx

import (
	"context"
	"errors"
	"testing"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/stretchr/testify/require"
)

type txMarker struct {
	ID    int    `db:"id"`
	Value string `db:"value"`
}

func TestPgxTxCommitsOnNilError(t *testing.T) {
	ctx := context.Background()
	pool := newTxTestPool(t)

	err := InPgxTx(ctx, pool, func(tx Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ($1)", "committed")
		return err
	})
	require.NoError(t, err)

	var got string
	require.NoError(t, pool.QueryRow(ctx, "SELECT value FROM dbx_tx_test").Scan(&got))
	require.Equal(t, "committed", got)
}

func TestPgxTxRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	pool := newTxTestPool(t)
	wantErr := errors.New("stop")

	err := InPgxTx(ctx, pool, func(tx Tx) error {
		_, execErr := tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ($1)", "rolled-back")
		require.NoError(t, execErr)
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)

	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM dbx_tx_test").Scan(&count))
	require.Zero(t, count)
}

func TestPgxTxRollsBackOnPanic(t *testing.T) {
	ctx := context.Background()
	pool := newTxTestPool(t)

	require.PanicsWithValue(t, "boom", func() {
		_ = InPgxTx(ctx, pool, func(tx Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ($1)", "panic")
			require.NoError(t, err)
			panic("boom")
		})
	})

	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM dbx_tx_test").Scan(&count))
	require.Zero(t, count)
}

func TestPgxTxWithResultReturnsValue(t *testing.T) {
	ctx := context.Background()
	pool := newTxTestPool(t)

	got, err := InPgxTxWithResult(ctx, pool, func(tx Tx) (txMarker, error) {
		_, err := tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ($1)", "result")
		require.NoError(t, err)

		var marker txMarker
		err = pgxscan.Get(ctx, tx, &marker, "SELECT id, value FROM dbx_tx_test WHERE value = $1", "result")
		return marker, err
	})
	require.NoError(t, err)
	require.Equal(t, "result", got.Value)
	require.NotZero(t, got.ID)
}

func newTxTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx := context.Background()
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(ctx, `
		CREATE TABLE dbx_tx_test (
			id BIGSERIAL PRIMARY KEY,
			value TEXT NOT NULL
		)
	`)
	require.NoError(t, err)
	return pool
}
