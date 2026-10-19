package dbtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

const insertCollectionBundleVideo = `INSERT INTO youtube_collection_targets
	(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,created_at)
	VALUES (101,'concurrent','video_list',40,120000,true,'2026-01-02')`

const insertCollectionBundleShorts = `INSERT INTO youtube_collection_targets
	(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,created_at)
	VALUES (101,'concurrent','shorts_list',40,60000,true,'2026-01-01')`

func TestCollectionRuntimeBundlesConcurrentWriters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		first  string
		second string
	}{
		{"distinct_absent_kinds", insertCollectionBundleVideo, insertCollectionBundleShorts},
		{
			"distinct_existing_kinds", `UPDATE youtube_collection_targets SET poll_interval_ms=1000 WHERE observation_kind='video_list'`,
			`UPDATE youtube_collection_targets SET created_at='2025-01-01' WHERE observation_kind='shorts_list'`,
		},
		{"header_then_target", `UPDATE youtube_collection_projection_generations SET status='RETIRED' WHERE generation=101`, insertCollectionBundleVideo},
		{"target_then_header", insertCollectionBundleVideo, `UPDATE youtube_collection_projection_generations SET status='RETIRED' WHERE generation=101`},
		{"legacy_guard_then_target", `SELECT guard_key FROM youtube_collection_projection_guard WHERE guard_key FOR UPDATE`, insertCollectionBundleVideo},
		{"collector_then_target", `SELECT generation FROM lock_current_youtube_collection_projection()`, insertCollectionBundleVideo},
		{"writer_then_legacy_guard", insertCollectionBundleVideo, `SELECT guard_key FROM youtube_collection_projection_guard WHERE guard_key FOR UPDATE`},
		{"writer_then_collector", insertCollectionBundleVideo, `SELECT generation FROM lock_current_youtube_collection_projection()`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := NewPool(t)
			seedCollectionBundleHeaders(t, pool)
			seedCollectionBundleTargets(t, pool)
			runCollectionBundleBlockedWriter(t, pool, tc.first, tc.second, nil)
		})
	}
}

// 실제 backend blocker를 확인하므로 스케줄링 지연만으로 직렬화 성공을 추정하지 않는다.
func runCollectionBundleBlockedWriter(t *testing.T, pool *pgxpool.Pool, firstSQL, secondSQL string, blocked func(pgx.Tx)) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	first, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, first)

	second, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, second)

	_, err = first.Exec(ctx, firstSQL)
	require.NoError(t, err)

	result := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, writeErr := second.Exec(ctx, secondSQL)
		result <- writeErr
	}()

	defer func() {
		cancel()
		rollbackLiveReviewReceiptFactsTx(t, first)
		<-done
	}()

	waitSecondTxBlockedByFirst(ctx, t, pool, first, second)

	if blocked != nil {
		blocked(first)
	}

	require.NoError(t, first.Commit(ctx))

	select {
	case err = <-result:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("collection writer did not finish after projection guard release")
	}

	assertCollectionRuntimeBundles(t, second)
	require.NoError(t, second.Commit(ctx))
}

func TestCollectionRuntimeBundlesStaleFixedSnapshotFails(t *testing.T) {
	for _, isolation := range []pgx.TxIsoLevel{pgx.RepeatableRead, pgx.Serializable} {
		t.Run(string(isolation), func(t *testing.T) {
			pool := NewPool(t)
			seedCollectionBundleHeaders(t, pool)

			stale, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: isolation})
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, stale)

			var before int

			require.NoError(t, stale.QueryRow(t.Context(), `SELECT count(*) FROM youtube_collection_runtime_bundles`).Scan(&before))
			execCollectionBundleSQL(t, pool, insertCollectionBundleVideo)

			_, err = stale.Exec(t.Context(), insertCollectionBundleShorts)

			pgErr, ok := errors.AsType[*pgconn.PgError](err)
			require.True(t, ok, "fixed snapshot must fail with PostgreSQL error: %v", err)
			require.Equal(t, "40001", pgErr.Code)
			require.NoError(t, pgxdb.Rollback(t.Context(), stale))
			assertCollectionRuntimeBundles(t, pool)

			var count int

			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_collection_targets WHERE subject_key='concurrent'`).Scan(&count))
			require.Equal(t, 1, count, "오래된 snapshot의 두 번째 원본은 커밋되지 않습니다")
		})
	}
}

func TestCollectionRuntimeBundlesMissingGuardRejectsSourceWrite(t *testing.T) {
	pool := NewPool(t)
	seedCollectionBundleHeaders(t, pool)
	execCollectionBundleSQL(t, pool, `DELETE FROM youtube_collection_projection_guard`)

	_, err := pool.Exec(t.Context(), insertCollectionBundleVideo)
	require.Error(t, err)
	assertCollectionRuntimeBundles(t, pool)

	var targets, guards int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM youtube_collection_targets),
		(SELECT count(*) FROM youtube_collection_projection_guard)`).Scan(&targets, &guards))
	require.Zero(t, targets, "guard 없는 원본 쓰기는 실패해야 합니다")
	require.Zero(t, guards, "누락된 guard를 쓰기 경로가 복구하지 않습니다")
}
