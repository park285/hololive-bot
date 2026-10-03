package dbtest

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

const collectionMembershipMigration = "259_youtube_collection_job_membership.sql"

// TestCollectionMembershipMigrationInitializesOnlyCurrent는 재적용 가능한 259가 CURRENT의 bounded target만
// membership을 초기화하고 과거 generation은 backfill하지 않으며, 이전 lock 함수를 제거하는지 확인한다.
func TestCollectionMembershipMigrationInitializesOnlyCurrent(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('RETIRED',1,repeat('b',64),now()+interval '1 hour',now()-interval '1 hour'),
		       ('CURRENT',1,repeat('a',64),now()+interval '1 hour',now());
		INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until)
		SELECT generation,'channel:a','community_page',40,120000,true,now()+interval '1 hour'
		FROM youtube_collection_projection_generations;
	`)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)
	require.NoError(t, applyMigrationFile(ctx, pool, dir, collectionMembershipMigration))
	require.NoError(t, applyMigrationFile(ctx, pool, dir, collectionMembershipMigration))

	var currentInitialized, retiredNull, guards int

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE g.status='CURRENT' AND t.member_since_generation = g.generation),
		       count(*) FILTER (WHERE g.status='RETIRED' AND t.member_since_generation IS NULL),
		       (SELECT count(*) FROM youtube_collection_projection_guard)
		FROM youtube_collection_targets t
		JOIN youtube_collection_projection_generations g ON g.generation = t.projection_generation
	`).Scan(&currentInitialized, &retiredNull, &guards))
	require.Equal(t, 1, currentInitialized)
	require.Equal(t, 1, retiredNull)
	require.Equal(t, 1, guards)

	var oldLockPresent bool

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT to_regprocedure('public.lock_youtube_collection_projection(bigint)') IS NOT NULL`).Scan(&oldLockPresent))
	require.False(t, oldLockPresent)

	// 이전 collector가 만든 lease 기본값은 새 유효성 규칙에서 항상 거짓이다.
	var legacyValid bool

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT public.youtube_collection_membership_valid('{}'::text[], false, 'channel:a', 0, generation)
		FROM youtube_collection_projection_generations WHERE status='CURRENT'
	`).Scan(&legacyValid))
	require.False(t, legacyValid)
}

// TestLockCurrentProjectionWaitsForRefreshAndReadsNewCurrent는 refresh가 guard를 쥔 채 CURRENT를 교체하면
// collector lock 함수가 기다렸다가 0행이 아니라 새 CURRENT generation을 돌려주는지 확인한다.
func TestLockCurrentProjectionWaitsForRefreshAndReadsNewCurrent(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	var previous int64

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('CURRENT',0,repeat('a',64),now()+interval '1 hour',now())
		RETURNING generation
	`).Scan(&previous))

	refresh, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() {
		if rollbackErr := refresh.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback refresh guard transaction: %v", rollbackErr)
		}
	}()

	_, err = refresh.Exec(ctx, `SELECT guard_key FROM youtube_collection_projection_guard WHERE guard_key FOR UPDATE`)
	require.NoError(t, err)

	type lockResult struct {
		generation int64
		err        error
	}

	done := make(chan lockResult, 1)

	go func() {
		var result lockResult

		result.err = pool.QueryRow(ctx, `SELECT generation FROM public.lock_current_youtube_collection_projection()`).Scan(&result.generation)
		done <- result
	}()

	select {
	case result := <-done:
		t.Fatalf("collector lock did not wait for the refresh guard: %+v", result)
	case <-time.After(300 * time.Millisecond):
	}

	var next int64

	_, err = refresh.Exec(ctx, `UPDATE youtube_collection_projection_generations SET status='RETIRED' WHERE generation=$1`, previous)
	require.NoError(t, err)
	require.NoError(t, refresh.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('CURRENT',0,repeat('c',64),now()+interval '1 hour',now())
		RETURNING generation
	`).Scan(&next))
	require.NoError(t, refresh.Commit(ctx))

	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, next, result.generation)
	case <-time.After(5 * time.Second):
		t.Fatal("collector lock did not resume after the refresh committed")
	}
}

// TestLockCurrentProjectionFailsWithoutGuard는 guard row가 없을 때 잠금 없이 CURRENT를 돌려주지 않는지 확인한다.
func TestLockCurrentProjectionFailsWithoutGuard(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('CURRENT',0,repeat('a',64),now()+interval '1 hour',now());
		DELETE FROM youtube_collection_projection_guard;
	`)
	require.NoError(t, err)

	var generation int64

	err = pool.QueryRow(ctx, `SELECT generation FROM public.lock_current_youtube_collection_projection()`).Scan(&generation)
	require.ErrorContains(t, err, "guard row is missing")
}
