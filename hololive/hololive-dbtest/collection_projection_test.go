package dbtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type collectionBundleDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func execCollectionBundleSQL(t *testing.T, db collectionBundleDB, query string) {
	t.Helper()

	_, err := db.Exec(t.Context(), query)
	require.NoError(t, err)
}

func seedCollectionBundleHeaders(t *testing.T, db collectionBundleDB) {
	t.Helper()

	execCollectionBundleSQL(t, db, `INSERT INTO youtube_collection_projection_generations
		(generation,status,row_count,projection_sha256,valid_until,created_at,activated_at) OVERRIDING SYSTEM VALUE
		VALUES (101,'CURRENT',0,repeat('a',64),now()+interval '1 hour','2026-01-01','2026-01-02'),
		       (102,'STAGING',0,repeat('b',64),now()+interval '1 hour','2026-01-01',NULL),
		       (103,'RETIRED',0,repeat('c',64),'2026-01-03','2026-01-01','2026-01-02')`)
}

func seedCollectionBundleTargets(t *testing.T, db collectionBundleDB) {
	t.Helper()

	execCollectionBundleSQL(t, db, `INSERT INTO youtube_collection_targets
		(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,created_at,not_before,member_since_generation)
		SELECT header.generation,fixture.subject,fixture.kind,40,fixture.cadence,fixture.enabled,
		       fixture.created::timestamptz,fixture.eligible::timestamptz,header.generation
		FROM youtube_collection_projection_generations header
		CROSS JOIN (VALUES
		 ('channel','video_list',120000,true,'2026-01-04','2099-01-04'),
		 ('channel','shorts_list',60000,true,'2026-01-03',NULL),
		 ('channel','channel_profile',240000,true,'2026-01-02','2099-01-02'),
		 ('channel','channel_photo',180000,true,'2026-01-05','2099-01-01'),
		 ('channel','live_snapshot',30000,true,'2026-01-01',NULL),
		 ('channel','channel_live_check',45000,true,'2026-01-01',NULL),
		 ('video','video_live_check',15000,true,'2026-01-01',NULL),
		 ('channel','community_page',300000,true,'2026-01-01',NULL),
		 ('disabled','video_list',1000,false,'2025-01-01',NULL),
		 ('channel','schedule_snapshot',1000,true,'2025-01-01',NULL),
		 ('Channel','video_list',90000,true,'2026-01-01',NULL),
		 ('é','video_list',90000,true,'2026-01-01',NULL),
		 (U&'e\0301','video_list',90000,true,'2026-01-01',NULL)
		) fixture(subject,kind,cadence,enabled,created,eligible)`)
}

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

func TestProjectionRetentionPreservesBoundsAndProtection(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	seedCollectionBundleRetention(t, pool)

	protected := collectionBundleProtectedSources(t, pool)

	var expected [3]int64

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM youtube_collection_target_reasons WHERE projection_generation=103),
		(SELECT count(*) FROM youtube_collection_targets WHERE projection_generation=103),1::bigint`).Scan(&expected[0], &expected[1], &expected[2]))

	total := drainCollectionBundleRetention(t, pool)
	require.Equal(t, expected, total, "반환한 삭제 수는 실제 원본 삭제와 일치해야 합니다")
	require.Equal(t, protected, collectionBundleProtectedSources(t, pool), "CURRENT/STAGING, lease 참조, cutoff 이후 세대는 보존합니다")

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

func TestProjectionRetentionWaitsForProjectionWriterLock(t *testing.T) {
	pool := NewPool(t)
	seedCollectionBundleRetention(t, pool)
	runCollectionBundleBlockedWriter(t, pool, `SELECT lock_youtube_collection_runtime_bundles()`,
		`SELECT deleted_generations FROM delete_retired_youtube_projection_batch('2026-02-01',3)`, func(writer pgx.Tx) {
			execCollectionBundleSQL(t, writer, `SELECT generation FROM youtube_collection_projection_generations WHERE generation=103 FOR UPDATE NOWAIT;
				SELECT subject_key FROM youtube_collection_targets WHERE projection_generation=103 FOR UPDATE NOWAIT;
				SELECT reason_key FROM youtube_collection_target_reasons WHERE projection_generation=103 FOR UPDATE NOWAIT`)
		})
}

func TestProjectionDirectWritesWaitForCollectorSharedLock(t *testing.T) {
	for _, write := range []string{
		`UPDATE youtube_collection_targets SET enabled=false WHERE projection_generation=101`,
		`DELETE FROM youtube_collection_targets WHERE projection_generation=102`,
		`UPDATE youtube_collection_projection_generations SET valid_until=valid_until+interval '1 minute' WHERE generation=101`,
	} {
		pool := NewPool(t)
		seedCollectionBundleHeaders(t, pool)
		seedCollectionBundleTargets(t, pool)
		runCollectionBundleBlockedWriter(t, pool, `SELECT generation FROM lock_current_youtube_collection_projection()`, write, nil)
	}
}

func TestProjectionRetentionRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

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
		t.Fatal("collection writer did not finish after the projection writer lock was released")
	}

	require.NoError(t, second.Commit(ctx))
}

func TestProjectionLocksRequireReadCommitted(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)

	for _, query := range []string{
		`SELECT lock_youtube_collection_runtime_bundles()`,
		`SELECT generation FROM lock_current_youtube_collection_projection()`,
	} {
		tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		require.NoError(t, err)

		_, err = tx.Exec(t.Context(), query)
		require.ErrorContains(t, err, "require read committed transactions")
		require.NoError(t, tx.Rollback(t.Context()))
	}
}
