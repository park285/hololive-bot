package dbtest

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func seedCollectionBundleRetention(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	seedCollectionBundleHeaders(t, pool)
	execCollectionBundleSQL(t, pool, `UPDATE youtube_collection_projection_generations SET valid_until='2026-01-03';
		INSERT INTO youtube_collection_projection_generations
		(generation,status,row_count,projection_sha256,valid_until,activated_at) OVERRIDING SYSTEM VALUE
		VALUES (104,'RETIRED',0,repeat('d',64),'2026-01-02','2026-01-01'),
		       (105,'RETIRED',0,repeat('e',64),'2099-01-01','2026-01-01');
		INSERT INTO youtube_collection_job_leases(job_key,provider,job_class,collection_job_kind,subject_key,
			projection_generation,poll_interval_ms,scheduled_for,next_due_at)
		VALUES ('retention-lease','youtubejs','SUBJECT','youtubejs_content','channel',104,60000,now(),now())`)
	seedCollectionBundleTargets(t, pool)
	execCollectionBundleSQL(t, pool, `INSERT INTO youtube_collection_target_reasons
		(projection_generation,subject_key,observation_kind,reason_kind,reason_key)
		SELECT projection_generation,subject_key,observation_kind,'operational_roster',reason
		FROM youtube_collection_targets CROSS JOIN (VALUES ('a'),('b')) fixture(reason)`)
}

func collectionBundleProtectedSources(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var snapshot string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
		'headers',(SELECT jsonb_agg(to_jsonb(header) ORDER BY generation) FROM youtube_collection_projection_generations header WHERE generation<>103),
		'targets',(SELECT jsonb_agg(to_jsonb(target) ORDER BY projection_generation,subject_key COLLATE "C",observation_kind) FROM youtube_collection_targets target WHERE projection_generation<>103),
		'reasons',(SELECT jsonb_agg(to_jsonb(reason) ORDER BY projection_generation,subject_key COLLATE "C",observation_kind,reason_key) FROM youtube_collection_target_reasons reason WHERE projection_generation<>103),
		'leases',(SELECT jsonb_agg(to_jsonb(lease) ORDER BY job_key) FROM youtube_collection_job_leases lease))::text`).Scan(&snapshot))

	return snapshot
}

func TestCollectionRuntimeBundlesRetentionPreservesBoundsAndProtection(t *testing.T) {
	pool := NewPool(t)
	seedCollectionBundleRetention(t, pool)

	protected := collectionBundleProtectedSources(t, pool)
	bundles := collectionBundleTupleSnapshot(t, pool)

	var expected [3]int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM youtube_collection_target_reasons WHERE projection_generation=103),
		(SELECT count(*) FROM youtube_collection_targets WHERE projection_generation=103),1::bigint`).Scan(&expected[0], &expected[1], &expected[2]))

	total := drainCollectionBundleRetention(t, pool)
	require.Equal(t, expected, total, "반환한 삭제 수는 실제 원본 삭제와 일치해야 합니다")
	require.Equal(t, protected, collectionBundleProtectedSources(t, pool), "CURRENT/STAGING, lease 참조, cutoff 이후 세대는 보존합니다")
	require.Equal(t, bundles, collectionBundleTupleSnapshot(t, pool), "retired 정리는 활성 bundle을 다시 쓰지 않습니다")
	assertCollectionRuntimeBundles(t, pool)

	var remaining int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_collection_projection_generations WHERE generation=103`).Scan(&remaining))
	require.Zero(t, remaining)
}

func drainCollectionBundleRetention(t *testing.T, pool *pgxpool.Pool) [3]int64 {
	t.Helper()

	var total [3]int64

	for range 100 {
		var batch [3]int64

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT deleted_reasons,deleted_targets,deleted_generations FROM delete_retired_youtube_projection_batch('2026-02-01',3)`).
			Scan(&batch[0], &batch[1], &batch[2]))
		require.LessOrEqual(t, batch[0]+batch[1], int64(3), "reason cascade도 같은 배치 상한을 지켜야 합니다")
		require.LessOrEqual(t, batch[2], int64(1))

		if batch == [3]int64{} {
			return total
		}

		for i := range total {
			total[i] += batch[i]
		}
	}

	t.Fatal("bounded projection retention did not finish")

	return total
}

func TestCollectionRuntimeBundlesGuardPrecedesSourceLocks(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"retention", `SELECT * FROM delete_retired_youtube_projection_batch('2026-02-01',3)`},
		{"header", `UPDATE youtube_collection_projection_generations SET row_count=1 WHERE generation=103`},
		{"target", `UPDATE youtube_collection_targets SET poll_interval_ms=1000 WHERE projection_generation=103`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := NewPool(t)
			seedCollectionBundleRetention(t, pool)
			runCollectionBundleBlockedWriter(t, pool, `SELECT lock_youtube_collection_runtime_bundles()`, tc.sql, func(guard pgx.Tx) {
				// 대기 중인 writer가 원본을 먼저 잠갔다면 NOWAIT가 실패하여 guard/source 역순을 드러낸다.
				execCollectionBundleSQL(t, guard, `SELECT generation FROM youtube_collection_projection_generations WHERE generation=103 FOR UPDATE NOWAIT;
					SELECT subject_key FROM youtube_collection_targets WHERE projection_generation=103 FOR UPDATE NOWAIT;
					SELECT reason_key FROM youtube_collection_target_reasons WHERE projection_generation=103 FOR UPDATE NOWAIT`)
			})
		})
	}
}

func TestCollectionRuntimeBundlesRetentionRejectsInvalidRequests(t *testing.T) {
	pool := NewPool(t)

	for _, query := range []string{
		`SELECT * FROM delete_retired_youtube_projection_batch(NULL,1)`,
		`SELECT * FROM delete_retired_youtube_projection_batch(now(),NULL)`,
		`SELECT * FROM delete_retired_youtube_projection_batch(now(),0)`,
		`SELECT * FROM delete_retired_youtube_projection_batch(now(),1001)`,
	} {
		_, err := pool.Exec(t.Context(), query)

		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		require.True(t, ok, "invalid retention request must fail: %v", err)
		require.Equal(t, "22023", pgErr.Code)
	}
}
