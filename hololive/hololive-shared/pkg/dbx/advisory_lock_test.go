package dbx

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const advisoryLockTestKey int64 = 918273645

func TestWithSessionAdvisoryLockRunsFnAndReleases(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	conn := acquireAdvisoryLockTestConn(ctx, t, pool)

	called := false
	acquired, err := WithSessionAdvisoryLock(ctx, conn, advisoryLockTestKey, func(context.Context) error {
		called = true

		require.False(t, advisoryLockFree(ctx, t, pool))

		return nil
	})
	require.NoError(t, err)
	require.True(t, acquired)
	require.True(t, called)
	require.True(t, advisoryLockFree(ctx, t, pool))
}

func TestWithSessionAdvisoryLockSkipsFnWhenHeldElsewhere(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	holder := acquireAdvisoryLockTestConn(ctx, t, pool)

	var held bool

	require.NoError(t, holder.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockTestKey).Scan(&held))
	require.True(t, held)

	conn := acquireAdvisoryLockTestConn(ctx, t, pool)
	acquired, err := WithSessionAdvisoryLock(ctx, conn, advisoryLockTestKey, func(context.Context) error {
		t.Fatal("fn must not run without the lock")

		return nil
	})
	require.NoError(t, err)
	require.False(t, acquired)
}

func TestWithSessionAdvisoryLockReleasesAfterContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	pool := dbtest.NewPool(t)
	conn := acquireAdvisoryLockTestConn(ctx, t, pool)

	wantErr := errors.New("stop")
	acquired, err := WithSessionAdvisoryLock(ctx, conn, advisoryLockTestKey, func(context.Context) error {
		cancel()

		return wantErr
	})
	require.ErrorIs(t, err, wantErr)
	require.True(t, acquired)
	require.True(t, advisoryLockFree(t.Context(), t, pool))
}

func TestWithSessionAdvisoryLockFailsClosedWhenUnlockCannotBeConfirmed(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	conn := acquireAdvisoryLockTestConn(ctx, t, pool)
	wantErr := errors.New("callback failed")

	acquired, err := WithSessionAdvisoryLock(ctx, conn, advisoryLockTestKey, func(context.Context) error {
		var unlocked bool

		require.NoError(t, conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockTestKey).Scan(&unlocked))
		require.True(t, unlocked)

		return wantErr
	})
	require.True(t, acquired)
	require.ErrorIs(t, err, wantErr)
	require.ErrorIs(t, err, errSessionAdvisoryUnlock)
	require.True(t, advisoryLockFree(t.Context(), t, pool))
}

func TestWithSessionAdvisoryLockRejectsNilQuerier(t *testing.T) {
	acquired, err := WithSessionAdvisoryLock(t.Context(), nil, advisoryLockTestKey, nil)
	require.Error(t, err)
	require.False(t, acquired)
}

func acquireAdvisoryLockTestConn(ctx context.Context, t *testing.T, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(conn.Release)

	return conn
}

// pg_try_advisory_lock으로 확인하고 바로 해제한다. 검사 자체가 락을 남기면
// 뒤따르는 단언이 오염된다.
func advisoryLockFree(ctx context.Context, t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	var free bool

	require.NoError(t, conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockTestKey).Scan(&free))

	if free {
		_, err = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockTestKey)
		require.NoError(t, err)
	}

	return free
}
