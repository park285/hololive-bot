package dbx

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

type txMarker struct {
	ID    int    `db:"id"`
	Value string `db:"value"`
}

func TestPgxTxCommitsOnNilError(t *testing.T) {
	ctx := t.Context()
	pool := newTxTestPool(t)

	err := InPgxTx(ctx, pool, func(tx Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ($1)", "committed"); err != nil {
			return fmt.Errorf("insert committed row: %w", err)
		}

		return nil
	})
	require.NoError(t, err)

	var got string

	require.NoError(t, pool.QueryRow(ctx, "SELECT value FROM dbx_tx_test").Scan(&got))
	require.Equal(t, "committed", got)
}

func TestPgxTxRollsBackOnError(t *testing.T) {
	ctx := t.Context()
	pool := newTxTestPool(t)
	wantErr := errors.New("stop")

	err := InPgxTx(ctx, pool, func(tx Tx) error {
		_, execErr := tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ($1)", "rolled-back")
		require.NoError(t, execErr)

		return wantErr
	})
	require.ErrorIs(t, err, wantErr)
	require.EqualError(t, err, "run pgx transaction: stop")

	var count int

	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM dbx_tx_test").Scan(&count))
	require.Zero(t, count)
}

// 결과형 helper도 같은 트랜잭션 실패를 한 단계로만 감싸야 호출자가 붙인 동작 접두 뒤에 원인이 바로 보인다.
func TestPgxTxWithResultRollsBackWithSingleTransactionPrefix(t *testing.T) {
	ctx := t.Context()
	pool := newTxTestPool(t)
	wantErr := errors.New("stop")

	got, err := InPgxTxWithResult(ctx, pool, func(tx Tx) (int, error) {
		_, execErr := tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ($1)", "rolled-back")
		require.NoError(t, execErr)

		return 7, wantErr
	})
	require.ErrorIs(t, err, wantErr)
	require.EqualError(t, err, "run pgx transaction: stop")
	require.Equal(t, 7, got)

	var count int

	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM dbx_tx_test").Scan(&count))
	require.Zero(t, count)
}

func TestPgxTxRollsBackOnPanic(t *testing.T) {
	ctx := t.Context()
	pool := newTxTestPool(t)

	require.PanicsWithValue(t, "boom", func() {
		err := InPgxTx(ctx, pool, func(tx Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ($1)", "panic")
			require.NoError(t, err)
			panic("boom")
		})
		require.NoError(t, err)
	})

	var count int

	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM dbx_tx_test").Scan(&count))
	require.Zero(t, count)
}

func TestPgxTxCancellationRollsBackWithoutDiscardingConnection(t *testing.T) {
	pool := newTxTestPool(t)
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	defer conn.Release()

	pid := conn.Conn().PgConn().PID()
	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ('cancelled')")
	require.NoError(t, err)
	cancel()

	require.ErrorIs(t, finishPgxTx(ctx, tx, context.Canceled), context.Canceled)
	require.False(t, conn.Conn().IsClosed(), "rollback must not discard the connection on caller cancellation")
	require.Equal(t, pid, conn.Conn().PgConn().PID())

	var count int

	require.NoError(t, conn.QueryRow(t.Context(), "SELECT count(*) FROM dbx_tx_test").Scan(&count))
	require.Zero(t, count, "canceled transaction must not commit its writes")
}

func TestPgxTxWithResultReturnsValue(t *testing.T) {
	ctx := t.Context()
	pool := newTxTestPool(t)

	got, err := InPgxTxWithResult(ctx, pool, func(tx Tx) (txMarker, error) {
		_, err := tx.Exec(ctx, "INSERT INTO dbx_tx_test (value) VALUES ($1)", "result")
		require.NoError(t, err)

		var marker txMarker

		if err := pgxscan.Get(ctx, tx, &marker, "SELECT id, value FROM dbx_tx_test WHERE value = $1", "result"); err != nil {
			return marker, fmt.Errorf("select marker: %w", err)
		}

		return marker, nil
	})
	require.NoError(t, err)
	require.Equal(t, "result", got.Value)
	require.NotZero(t, got.ID)
}

func newTxTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx := t.Context()
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
