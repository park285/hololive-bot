package dbtest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

// 기본 권한이 넓어도 runtime과 별도 기록 역할만 refresh를 실행할 수 있습니다.
func TestLiveRuntimeRefreshPrivileges(t *testing.T) {
	t.Parallel()

	pool := NewBlankPool(t)
	roles := createObservationGrantRoles(t, pool)
	// 기록 역할의 기본 실행 권한이 새 의존 함수에도 이어지는지 확인합니다.
	for _, role := range []string{roles.runtime, roles.scraper, "pg_monitor"} {
		_, err := pool.Exec(t.Context(), "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO "+pgx.Identifier{role}.Sanitize())
		require.NoError(t, err)
	}

	applyBaselineForRoles(t, pool, roles)

	var recorderDependencies bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
 has_function_privilege('pg_monitor','record_youtube_live_review(uuid,text,text,text,text)','EXECUTE')
 AND has_function_privilege('pg_monitor','refresh_youtube_live_runtime_state(text[])','EXECUTE')
 AND has_function_privilege('pg_monitor','lock_youtube_live_runtime_state()','EXECUTE')
 AND has_function_privilege('pg_monitor','youtube_live_review_receipt_value(jsonb)','EXECUTE')`).Scan(&recorderDependencies))
	require.True(t, recorderDependencies)
	assertLiveRuntimeRefreshPrivileges(t, pool, roles)
	assertRuntimeRoleRefreshesLiveRuntimeState(t, pool, pgx.Identifier{roles.runtime}.Sanitize())
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
		require.Equal(t, role == roles.runtime, refresh, role)
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
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,lifecycle_origin) VALUES ('runtime-role','runtime-channel','LIVE','observed');
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
	t.Parallel()

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
