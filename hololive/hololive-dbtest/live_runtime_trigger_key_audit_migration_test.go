package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

const liveRuntimeTriggerKeyAuditMigration = "325_youtube_live_runtime_trigger_key_audit.sql"

func TestLiveRuntimeTriggerKeyAuditMigrationPreservesDataAndSecurity(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveRuntimeTriggerKeyAuditMigration)
	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)

	beforeState, beforeReceipts := liveReviewComparisonState(t, pool)
	beforeSecurity := liveRuntimeRefreshSecurity(t, pool)

	for range 2 {
		require.NoError(t, applyMigrationFile(t.Context(), pool, dir, liveRuntimeTriggerKeyAuditMigration))
		assertLiveRuntimeState(t, pool)

		afterState, afterReceipts := liveReviewComparisonState(t, pool)
		require.JSONEq(t, beforeState, afterState)
		require.Equal(t, beforeReceipts, afterReceipts)
		require.JSONEq(t, beforeSecurity, liveRuntimeRefreshSecurity(t, pool))
	}
}

// 트리거 함수의 SET search_path 절이 함수 종료 때 되돌리는 설정은 search_path뿐이어야 한다.
// 소유자 권한 트리거가 남긴 key는 writer 역할의 같은 트랜잭션에서 읽히고, 트랜잭션·savepoint 경계에서 사라진다.
func TestLiveRuntimeTriggerKeysLastUntilTransactionEnd(t *testing.T) {
	pool := NewPool(t)
	roles := createObservationGrantRoles(t, pool)
	runtime := pgx.Identifier{roles.runtime}.Sanitize()

	_, err := pool.Exec(t.Context(), `GRANT SELECT,INSERT,UPDATE ON youtube_live_sessions TO `+runtime)
	require.NoError(t, err)

	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(conn.Release)

	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(t.Context(), `SET LOCAL ROLE `+runtime)
	require.NoError(t, err)
	require.Empty(t, liveRuntimeTriggerKeys(t, tx))

	_, err = tx.Exec(t.Context(), `INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('key-b','key-channel','LIVE');
		INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('key-a','key-channel','UPCOMING');
		UPDATE youtube_live_sessions SET status='LIVE' WHERE video_id='key-b'`)
	require.NoError(t, err)
	require.Equal(t, []string{"key-a", "key-b"}, liveRuntimeTriggerKeys(t, tx), "문장마다 누적하고 중복 key는 한 번만 남깁니다")

	_, err = tx.Exec(t.Context(), `SAVEPOINT nested;
		INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('key-c','key-channel','LIVE')`)
	require.NoError(t, err)
	require.Equal(t, []string{"key-a", "key-b", "key-c"}, liveRuntimeTriggerKeys(t, tx))

	_, err = tx.Exec(t.Context(), `ROLLBACK TO SAVEPOINT nested`)
	require.NoError(t, err)
	require.Equal(t, []string{"key-a", "key-b"}, liveRuntimeTriggerKeys(t, tx), "savepoint 롤백은 그 안의 key도 되돌립니다")
	require.NoError(t, tx.Commit(t.Context()))
	assertLiveRuntimeState(t, pool)

	next, err := conn.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, next)

	require.Empty(t, liveRuntimeTriggerKeys(t, next), "커밋한 key는 같은 연결의 다음 트랜잭션에 남지 않습니다")

	// 소비·검토 입력이 아닌 열만 바꾼 UPDATE는 refresh하지 않으므로 key도 남기지 않는다.
	tag, err := next.Exec(t.Context(), `UPDATE youtube_live_sessions SET last_seen_at=now() WHERE video_id IN ('key-a','key-b')`)
	require.NoError(t, err)
	require.Equal(t, int64(2), tag.RowsAffected())
	require.Empty(t, liveRuntimeTriggerKeys(t, next))

	_, err = next.Exec(t.Context(), `INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('key-rolled-back','key-channel','LIVE')`)
	require.NoError(t, err)
	require.Equal(t, []string{"key-rolled-back"}, liveRuntimeTriggerKeys(t, next))
	require.NoError(t, pgxdb.Rollback(t.Context(), next))

	require.Empty(t, liveRuntimeTriggerKeys(t, conn), "롤백한 key는 같은 연결에 남지 않습니다")
}

func liveRuntimeTriggerKeys(t *testing.T, db liveRuntimeStateQuerier) []string {
	t.Helper()

	var keys []string

	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT COALESCE(NULLIF(current_setting('hololive.live_runtime_trigger_keys', true), '')::text[], ARRAY[]::text[])`).Scan(&keys))

	return keys
}
