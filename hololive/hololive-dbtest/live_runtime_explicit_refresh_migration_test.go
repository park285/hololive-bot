package dbtest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

const liveRuntimeExplicitRefreshMigration = "328_youtube_live_runtime_explicit_refresh.sql"

// 트리거 기간에 어긋난 행도 전체 재계산으로 맞추고, 재적용해도 원본·영수증과 권한 경계가 같아야 한다.
func TestLiveRuntimeExplicitRefreshMigrationRecomputesAndRemovesTriggers(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveRuntimeExplicitRefreshMigration)
	ctx := t.Context()
	roles := createObservationGrantRoles(t, pool)
	runtime := pgx.Identifier{roles.runtime}.Sanitize()
	scraper := pgx.Identifier{roles.scraper}.Sanitize()

	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)

	// scraper 대역을 기존 별도 기록 역할로도 써서 기록 함수가 부르는 refresh 권한 보충을 확인한다.
	_, err := pool.Exec(ctx, `GRANT EXECUTE ON FUNCTION record_youtube_live_review(uuid,text,text,text,text) TO `+scraper+`;
		UPDATE youtube_live_runtime_state SET channel_id='corrupted' WHERE video_id='array'`)
	require.NoError(t, err)
	require.Equal(t, [3]int64{0, 0, 1}, LiveRuntimeStateDrift(t, pool))

	beforeState, beforeReceipts := liveReviewComparisonState(t, pool)
	beforeRecordACL := liveReviewRecordACL(t, pool)

	raw, err := fs.ReadFile(os.DirFS(dir), liveRuntimeExplicitRefreshMigration)
	require.NoError(t, err)

	migration := strings.NewReplacer("hololive_runtime", roles.runtime, "hololive_scraper", roles.scraper).Replace(string(raw))

	for range 2 {
		require.NoError(t, applyMigrationContent(ctx, pool, liveRuntimeExplicitRefreshMigration, migration))
		assertLiveRuntimeState(t, pool)

		afterState, afterReceipts := liveReviewComparisonState(t, pool)
		require.JSONEq(t, beforeState, afterState)
		require.Equal(t, beforeReceipts, afterReceipts)
		require.Equal(t, beforeRecordACL, liveReviewRecordACL(t, pool))
	}

	assertLiveRuntimeTriggersRemoved(t, pool)
	assertLiveRuntimeRefreshPrivileges(t, pool, roles)
	assertRuntimeRoleRefreshesLiveRuntimeState(t, pool, runtime)
}

func assertLiveRuntimeTriggersRemoved(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var triggers int

	var guardTable, statementFunction, guardFunction bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT count(oid) FROM pg_trigger WHERE tgname LIKE 'youtube_live_runtime%'),
		to_regclass('public.youtube_live_runtime_state_guard') IS NOT NULL,
		to_regprocedure('public.refresh_youtube_live_runtime_state_statement()') IS NOT NULL,
		to_regprocedure('public.guard_youtube_live_runtime_state()') IS NOT NULL`).
		Scan(&triggers, &guardTable, &statementFunction, &guardFunction))
	require.Zero(t, triggers)
	require.False(t, guardTable)
	require.False(t, statementFunction)
	require.False(t, guardFunction)
}

func assertLiveRuntimeRefreshPrivileges(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) {
	t.Helper()

	for _, role := range []string{roles.runtime, roles.scraper} {
		var write, refresh, shared bool

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT
			has_table_privilege($1,'youtube_live_runtime_state','INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'),
			has_function_privilege($1,'refresh_youtube_live_runtime_state(text[])','EXECUTE'),
			has_function_privilege($1,'lock_youtube_live_runtime_state_shared()','EXECUTE')`, role).
			Scan(&write, &refresh, &shared))
		require.False(t, write, role)
		require.True(t, refresh, "관측 writer와 기록 역할은 refresh를 실행해야 합니다: %s", role)
		require.Equal(t, role == roles.runtime, shared, role)
	}
}

// runtime 역할은 수명 모델 표를 직접 쓰지 않고도 shared 잠금 뒤 refresh로 자기 원본 쓰기를 반영한다.
func assertRuntimeRoleRefreshesLiveRuntimeState(t *testing.T, pool *pgxpool.Pool, runtime string) {
	t.Helper()

	ctx := t.Context()
	_, err := pool.Exec(ctx, `GRANT SELECT,INSERT ON youtube_live_sessions TO `+runtime)
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(ctx, `SET LOCAL ROLE `+runtime+`;
		SELECT lock_youtube_live_runtime_state_shared();
		INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('runtime-role','runtime-channel','LIVE');
		SELECT refresh_youtube_live_runtime_state(ARRAY['runtime-role']);
		RESET ROLE`)
	require.NoError(t, err)

	var visible bool

	require.NoError(t, tx.QueryRow(ctx, `SELECT session_status='LIVE' FROM youtube_live_runtime_state WHERE video_id='runtime-role'`).Scan(&visible))
	require.True(t, visible)
	assertLiveRuntimeState(t, tx)
}

// shared 보유자끼리는 서로 기다리지 않고, exclusive 요청은 shared 보유자가 끝날 때까지 기다린다.
func TestLiveRuntimeGlobalLockSharedAndExclusive(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	holder, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, holder)

	_, err = holder.Exec(ctx, `SELECT lock_youtube_live_runtime_state_shared()`)
	require.NoError(t, err)

	require.NoError(t, acquireLiveRuntimeLockWithTimeout(t, pool, "lock_youtube_live_runtime_state_shared"))

	err = acquireLiveRuntimeLockWithTimeout(t, pool, "lock_youtube_live_runtime_state")

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "exclusive request must time out behind a shared holder: %v", err)
	require.Equal(t, "55P03", pgErr.Code)

	require.NoError(t, pgxdb.Rollback(ctx, holder))
	require.NoError(t, acquireLiveRuntimeLockWithTimeout(t, pool, "lock_youtube_live_runtime_state"))
}

func acquireLiveRuntimeLockWithTimeout(t *testing.T, pool *pgxpool.Pool, function string) error {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	if _, execErr := tx.Exec(t.Context(), `SET LOCAL lock_timeout='200ms'; SELECT `+function+`()`); execErr != nil {
		return fmt.Errorf("acquire %s: %w", function, execErr)
	}

	return nil
}

func liveReviewRecordACL(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var acl string

	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT proacl::text FROM pg_proc WHERE oid='record_youtube_live_review(uuid,text,text,text,text)'::regprocedure`).Scan(&acl))

	return acl
}
