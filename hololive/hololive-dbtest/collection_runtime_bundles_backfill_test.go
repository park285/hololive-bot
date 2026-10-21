package dbtest

import (
	"context"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestCollectionRuntimeBundlesBackfillExcludesConcurrentWrites(t *testing.T) {
	t.Parallel()

	pool, dir := projectionMigrationPoolBefore(t, collectionRuntimeBundlesMigration)
	seedCollectionBundleHeaders(t, pool)

	raw, err := fs.ReadFile(os.DirFS(dir), collectionRuntimeBundlesMigration)
	require.NoError(t, err)

	for _, sourceWrite := range []string{insertCollectionBundleVideo, insertCollectionBundleShorts} {
		// 첫 설치는 trigger 없는 writer, 재적용은 기존 guard를 가진 writer와 겹치게 한다.
		applyCollectionBundleMigrationBehindWriter(t, pool, sourceWrite, string(raw))
		assertCollectionRuntimeBundles(t, pool)
	}
}

func applyCollectionBundleMigrationBehindWriter(t *testing.T, pool *pgxpool.Pool, sourceWrite, migration string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	writer, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, writer)

	_, err = writer.Exec(ctx, sourceWrite)
	require.NoError(t, err)

	migrator, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer migrator.Release()

	result := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, migrationErr := migrator.Exec(ctx, migration)
		result <- migrationErr
	}()

	defer func() {
		cancel()
		rollbackLiveReviewReceiptFactsTx(t, writer)
		<-done
	}()

	writerPID := writer.Conn().PgConn().PID()
	migratorPID := migrator.Conn().PgConn().PID()

	require.Eventually(t, func() bool {
		var blocked bool

		queryErr := pool.QueryRow(ctx, `SELECT $1::int=ANY(pg_blocking_pids($2::int))`, writerPID, migratorPID).Scan(&blocked)

		return queryErr == nil && blocked
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, writer.Commit(ctx))

	select {
	case err = <-result:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("runtime bundle migration did not resume after source writer committed")
	}
}
