package dbtest

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

const (
	liveRuntimeStateMigration = "312_youtube_live_runtime_state.sql"
	// 현재 일치 영수증과 함께 seedLiveReviewComparison이 만드는 영상이다.
	liveRuntimeReviewedVideo = "array"
)

func assertLiveRuntimeState(t *testing.T, db LiveRuntimeStateReader) {
	t.Helper()

	require.Equal(t, [3]int64{}, LiveRuntimeStateDrift(t, db), "누락·잉여·값 변경")
}

// 진단 SQL이 늘 0을 내면 운영 점검도 정상으로 오판하므로, 집계를 직접 손상시켜 종류마다 한 건씩 세는지 확인한다.
func TestLiveRuntimeStateDriftCountsEachMismatch(t *testing.T) {
	pool := NewPool(t)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(t.Context(), `INSERT INTO youtube_live_sessions(video_id,channel_id,status)
		VALUES ('drift-missing','drift-channel','LIVE'),('drift-changed','drift-channel','LIVE');
		SELECT refresh_youtube_live_runtime_state(ARRAY['drift-missing','drift-changed']);
		DELETE FROM youtube_live_runtime_state WHERE video_id='drift-missing';
		UPDATE youtube_live_runtime_state SET channel_id='corrupted' WHERE video_id='drift-changed';
		INSERT INTO youtube_live_runtime_state(video_id,channel_id,session_status) VALUES ('drift-extra','drift-channel','LIVE')`)
	require.NoError(t, err)

	require.Equal(t, [3]int64{1, 1, 1}, LiveRuntimeStateDrift(t, tx), "누락·잉여·값 변경")
}

func TestLiveRuntimeStateMigrationBackfillAndReapply(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveRuntimeStateMigration)
	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)
	seedLiveReviewTypedFacts(t, pool)

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,lifecycle_origin)
		VALUES ('ended-peer','runtime-channel','ENDED','observed'),('metadata-peer','runtime-channel','UPCOMING','metadata_only'),
		       ('live-peer','runtime-channel','LIVE','observed');
		INSERT INTO youtube_live_reconciliation_heads(video_id,status)
		VALUES ('orphan','LIVE'),('ended-peer','UPCOMING'),('inactive-orphan','ENDED'),('live-peer','ENDED')`)
	require.NoError(t, err)

	_, beforeReceipts := liveReviewComparisonState(t, pool)

	for range 2 {
		require.NoError(t, applyMigrationFile(t.Context(), pool, dir, liveRuntimeStateMigration))
		assertLiveRuntimeState(t, pool)

		_, afterReceipts := liveReviewComparisonState(t, pool)
		require.Equal(t, beforeReceipts, afterReceipts)

		var latestMatching bool

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT reviewed_at='2026-10-01T02:00:00Z'::timestamptz
			FROM youtube_live_runtime_state WHERE video_id='array'`).Scan(&latestMatching))
		require.True(t, latestMatching, "더 늦은 불일치 영수증은 일치 영수증을 가리면 안 됩니다")
	}
}

// 원본 변경마다 바뀐 이전·새 key로 갱신하면 같은 트랜잭션에서 오라클과 일치하고, rollback은 갱신도 함께 되돌린다.
func TestLiveRuntimeStateRefreshAppliesSourceChangesAndRollsBack(t *testing.T) {
	pool := NewPool(t)
	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)
	RefreshAllLiveRuntimeState(t, pool)
	assertLiveRuntimeState(t, pool)

	for _, tc := range []struct {
		name string
		sql  string
		keys []string
	}{
		{"session_semantic", `UPDATE youtube_live_sessions SET title='changed' WHERE video_id='array'`, []string{liveRuntimeReviewedVideo}},
		{"session_move", `UPDATE youtube_live_sessions SET video_id='moved-session' WHERE video_id='array'`, []string{liveRuntimeReviewedVideo, "moved-session"}},
		{"head_move", `UPDATE youtube_live_reconciliation_heads SET video_id='moved-head' WHERE video_id='array'`, []string{liveRuntimeReviewedVideo, "moved-head"}},
		{"availability_move", `UPDATE youtube_video_availability SET video_id='moved-availability' WHERE video_id='array'`, []string{liveRuntimeReviewedVideo, "moved-availability"}},
		{"pending_move", `UPDATE youtube_live_pending_ends SET video_id='headless' WHERE video_id='recorded'`, []string{"recorded", "headless"}},
		{"pending_semantic", `UPDATE youtube_live_pending_ends SET negative_eligible=false WHERE video_id='recorded'`, []string{"recorded"}},
		{"positive", `UPDATE youtube_live_reconciliation_heads SET last_upcoming_positive_at='2026-10-10',last_upcoming_positive_seen_at='2026-10-11' WHERE video_id='array'`, []string{liveRuntimeReviewedVideo}},
		{"availability_clock", `UPDATE youtube_video_availability SET scheduled_for='2026-08-01',effective_at='2026-08-01',observed_at='2026-08-02' WHERE video_id='recorded'`, []string{"recorded"}},
		{"session_delete", `DELETE FROM youtube_live_sessions WHERE video_id='array'`, []string{liveRuntimeReviewedVideo}},
		{"head_delete", `DELETE FROM youtube_live_reconciliation_heads WHERE video_id='array'`, []string{liveRuntimeReviewedVideo}},
		{"pending_delete", `DELETE FROM youtube_live_pending_ends WHERE video_id='recorded'`, []string{"recorded"}},
		{"availability_delete", `DELETE FROM youtube_video_availability WHERE video_id='array'`, []string{liveRuntimeReviewedVideo}},
		{"inactive_delete", `UPDATE youtube_live_sessions SET status='ENDED' WHERE video_id='array'; UPDATE youtube_live_reconciliation_heads SET status='ENDED' WHERE video_id='array'`, []string{liveRuntimeReviewedVideo}},
		{"absent_session_upsert", `INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('absent','runtime-channel','LIVE') ON CONFLICT(video_id) DO UPDATE SET status=excluded.status`, []string{"absent"}},
		{"absent_head_upsert", `INSERT INTO youtube_live_reconciliation_heads(video_id,status) VALUES ('absent','UPCOMING') ON CONFLICT(video_id) DO UPDATE SET status=excluded.status`, []string{"absent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, tx)

			_, err = tx.Exec(t.Context(), tc.sql)
			require.NoError(t, err)

			_, err = tx.Exec(t.Context(), `SELECT refresh_youtube_live_runtime_state($1::text[])`, tc.keys)
			require.NoError(t, err)
			assertLiveRuntimeState(t, tx)
			require.NoError(t, pgxdb.Rollback(t.Context(), tx))
			assertLiveRuntimeState(t, pool)
		})
	}
}

func TestLiveRuntimeStateReceiptInsertionChangedAndRevertedFacts(t *testing.T) {
	pool := NewPool(t)
	seedLiveReviewComparison(t, pool)
	RefreshAllLiveRuntimeState(t, pool)

	ctx := t.Context()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(ctx, `SELECT record_youtube_live_review(gen_random_uuid(),'none',snapshot.snapshot_sha256,'test-operator','동기 검토')
		FROM youtube_live_review_snapshot('none') snapshot`)
	require.NoError(t, err)
	assertLiveRuntimeState(t, tx)

	var reviewed bool

	require.NoError(t, tx.QueryRow(ctx, `SELECT reviewed_at IS NOT NULL FROM youtube_live_runtime_state WHERE video_id='none'`).Scan(&reviewed))
	require.True(t, reviewed)

	for _, title := range []string{"changed", "legacy"} {
		_, err = tx.Exec(ctx, `UPDATE youtube_live_sessions SET title=$1 WHERE video_id='none'`, title)
		require.NoError(t, err)

		_, err = tx.Exec(ctx, `SELECT refresh_youtube_live_runtime_state(ARRAY['none'])`)
		require.NoError(t, err)
		assertLiveRuntimeState(t, tx)
		require.NoError(t, tx.QueryRow(ctx, `SELECT reviewed_at IS NOT NULL FROM youtube_live_runtime_state WHERE video_id='none'`).Scan(&reviewed))
		require.Equal(t, title == "legacy", reviewed)
	}

	_, err = tx.Exec(ctx, `UPDATE youtube_live_review_receipt_facts SET facts='{}' WHERE video_id='none'`)
	require.ErrorContains(t, err, "append-only")
	require.NoError(t, pgxdb.Rollback(ctx, tx))
	assertLiveRuntimeState(t, pool)
	require.NoError(t, pool.QueryRow(ctx, `SELECT reviewed_at IS NOT NULL FROM youtube_live_runtime_state WHERE video_id='none'`).Scan(&reviewed))
	require.False(t, reviewed)
}

// 소비자 의미가 같은 기술적 쓰기 뒤 갱신은 행 버전을 새로 만들지 않아 읽기 모델의 불필요한 쓰기를 늘리지 않는다.
func TestLiveRuntimeStateRefreshKeepsUnchangedRow(t *testing.T) {
	pool := NewPool(t)
	seedLiveReviewComparison(t, pool)
	RefreshAllLiveRuntimeState(t, pool)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	var stateBefore, stateAfter string

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT ctid::text FROM youtube_live_runtime_state WHERE video_id='array'`).Scan(&stateBefore))

	_, err = tx.Exec(t.Context(), `
		UPDATE youtube_live_reconciliation_heads SET ignored_absence_scheduled_for=array_append(ignored_absence_scheduled_for,now()) WHERE video_id='array';
		UPDATE youtube_live_sessions SET last_seen_at=now() WHERE video_id='array';
		UPDATE youtube_video_availability SET received_at=now() WHERE video_id='array';
		SELECT refresh_youtube_live_runtime_state(ARRAY['array','array'])`)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT ctid::text FROM youtube_live_runtime_state WHERE video_id='array'`).Scan(&stateAfter))
	require.Equal(t, stateBefore, stateAfter, "기술적 쓰기는 동일한 소비자 행을 다시 쓰지 않습니다")
	assertLiveRuntimeState(t, tx)
}

func TestLiveRuntimeStateMigrationPrivilegesAndRecordingACL(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveRuntimeStateMigration)
	roles := createObservationGrantRoles(t, pool)
	runtime := pgx.Identifier{roles.runtime}.Sanitize()
	scraper := pgx.Identifier{roles.scraper}.Sanitize()

	// scraper 대역을 기존 별도 기록 역할로도 써서 기존 ACL 보존과 guard 의존 권한을 확인한다.
	_, err := pool.Exec(t.Context(), `ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO `+runtime+`, `+scraper+`;
		ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO `+runtime+`, `+scraper+`;
		GRANT EXECUTE ON FUNCTION record_youtube_live_review(uuid,text,text,text,text) TO `+scraper)
	require.NoError(t, err)

	var beforeACL string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT proacl::text FROM pg_proc WHERE oid='record_youtube_live_review(uuid,text,text,text,text)'::regprocedure`).Scan(&beforeACL))

	raw, err := fs.ReadFile(os.DirFS(dir), liveRuntimeStateMigration)
	require.NoError(t, err)

	migration := strings.NewReplacer("hololive_runtime", roles.runtime, "hololive_scraper", roles.scraper).Replace(string(raw))

	for range 2 {
		require.NoError(t, applyMigrationContent(t.Context(), pool, liveRuntimeStateMigration, migration))
	}

	var afterACL string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT proacl::text FROM pg_proc WHERE oid='record_youtube_live_review(uuid,text,text,text,text)'::regprocedure`).Scan(&afterACL))
	require.Equal(t, beforeACL, afterACL)

	for _, role := range []string{roles.runtime, roles.scraper} {
		var read, write, guardTable, guardFunction, refresh, record bool

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT
			has_table_privilege($1,'youtube_live_runtime_state','SELECT'),
			has_table_privilege($1,'youtube_live_runtime_state','INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'),
			has_table_privilege($1,'youtube_live_runtime_state_guard','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'),
			has_function_privilege($1,'lock_youtube_live_runtime_state()','EXECUTE'),
			has_function_privilege($1,'refresh_youtube_live_runtime_state(text[])','EXECUTE'),
			has_function_privilege($1,'record_youtube_live_review(uuid,text,text,text,text)','EXECUTE')`,
			role).Scan(&read, &write, &guardTable, &guardFunction, &refresh, &record))
		require.Equal(t, role == roles.runtime, read)
		require.False(t, write)
		require.False(t, guardTable)
		require.True(t, guardFunction)
		require.False(t, refresh)
		require.Equal(t, role == roles.scraper, record)
	}

	_, err = pool.Exec(t.Context(), `GRANT INSERT ON youtube_live_sessions TO `+runtime)
	require.NoError(t, err)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(t.Context(), `SET LOCAL ROLE `+runtime+`;
		SELECT lock_youtube_live_runtime_state();
		INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('runtime-role','runtime-channel','LIVE')`)
	require.NoError(t, err)

	var visible bool

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT session_status='LIVE' FROM youtube_live_runtime_state WHERE video_id='runtime-role'`).Scan(&visible))
	require.True(t, visible)

	_, err = tx.Exec(t.Context(), `UPDATE youtube_live_runtime_state SET channel_id='corrupted' WHERE video_id='runtime-role'`)
	require.ErrorContains(t, err, "permission denied")
}

// 증거 FK의 SET NULL은 수명 모델 입력이 아니므로 retention 삭제는 갱신 key 없이도 오라클과 일치한다.
func TestLiveRuntimeStateObservationDeleteKeepsState(t *testing.T) {
	pool := NewPool(t)
	seedLiveReviewComparison(t, pool)
	RefreshAllLiveRuntimeState(t, pool)

	ctx := t.Context()
	_, err := pool.Exec(ctx, `WITH payload AS (
		INSERT INTO source_observation_payloads(observation_kind,schema_version,canonical_profile,payload_sha256,payload)
		VALUES ('community_page',1,'source-observation-canonical-json-v1',decode(repeat('b',64),'hex'),'{}') RETURNING id
	), observation AS (
		INSERT INTO source_observations(provider,observation_kind,subject_key,observation_key,schema_version,
			contract_generation,scheduled_for,observed_at,scope_sha256,completeness,continuity,payload_id,
			evidence_sha256,provenance_id,fence_epoch,projection_generation)
		SELECT 'youtubejs','community_page','runtime-fk',sha256('runtime-fk'::bytea),1,1,now(),now(),decode(repeat('a',64),'hex'),
			'COMPLETE','CONTIGUOUS',id,decode(repeat('c',64),'hex'),source_observation_provenance_id('runtime-fk','migration','community_collect'),1,1
		FROM payload RETURNING id
	) UPDATE youtube_video_availability SET observation_id=observation.id FROM observation WHERE video_id='array'`)
	require.NoError(t, err)
	assertLiveRuntimeState(t, pool)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(ctx, `DELETE FROM source_observations WHERE subject_key='runtime-fk'`)
	require.NoError(t, err)
	assertLiveRuntimeState(t, tx)

	var cleared, reviewed bool

	require.NoError(t, tx.QueryRow(ctx, `SELECT observation_id IS NULL FROM youtube_video_availability WHERE video_id='array'`).Scan(&cleared))
	require.True(t, cleared)
	require.NoError(t, tx.QueryRow(ctx, `SELECT reviewed_at IS NOT NULL FROM youtube_live_runtime_state WHERE video_id='array'`).Scan(&reviewed))
	require.True(t, reviewed, "증거 FK 제거는 가용성 의미 사실을 바꾸지 않습니다")
}

func TestLiveRuntimeStateMigrationFailureIsAtomic(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveRuntimeStateMigration)
	seedLiveReviewComparison(t, pool)

	beforeResults, beforeReceipts := liveReviewComparisonState(t, pool)

	// 이름 충돌로 마지막 backfill을 실패시켜 앞서 만든 guard·함수·trigger도 남지 않는지 확인한다.
	_, err := pool.Exec(t.Context(), `CREATE TABLE youtube_live_runtime_state(video_id integer PRIMARY KEY)`)
	require.NoError(t, err)
	require.Error(t, applyMigrationFile(t.Context(), pool, dir, liveRuntimeStateMigration))

	var guardMissing, triggerMissing bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_regclass('public.youtube_live_runtime_state_guard') IS NULL,
		NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='youtube_live_runtime_guard')`).Scan(&guardMissing, &triggerMissing))
	require.True(t, guardMissing)
	require.True(t, triggerMissing)

	afterResults, afterReceipts := liveReviewComparisonState(t, pool)
	require.JSONEq(t, beforeResults, afterResults)
	require.Equal(t, beforeReceipts, afterReceipts)
}

func TestLiveRuntimeStateRecordingValidationAndCAS(t *testing.T) {
	pool := NewPool(t)
	seedLiveReviewComparison(t, pool)
	RefreshAllLiveRuntimeState(t, pool)

	for _, tc := range []struct {
		name      string
		isolation pgx.TxIsoLevel
		video     string
		operator  string
		want      string
	}{
		{"isolation", pgx.Serializable, liveRuntimeReviewedVideo, "operator", "requires read committed"},
		{"validation", pgx.ReadCommitted, liveRuntimeReviewedVideo, " operator", "invalid live review request"},
		{"missing", pgx.ReadCommitted, "missing", "operator", "reviewed live session missing"},
		{"cas", pgx.ReadCommitted, liveRuntimeReviewedVideo, "operator", "snapshot changed or is not unresolved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: tc.isolation})
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, tx)

			_, err = tx.Exec(t.Context(), `SELECT record_youtube_live_review(gen_random_uuid(),$1,repeat('0',64),$2,'검토')`, tc.video, tc.operator)
			require.ErrorContains(t, err, tc.want)
			require.NoError(t, pgxdb.Rollback(t.Context(), tx))
			assertLiveRuntimeState(t, pool)
		})
	}
}

// 고정 snapshot은 잠금 대기 중 커밋된 원본을 놓치므로 refresh가 READ COMMITTED 밖에서 거부되어야 한다.
func TestLiveRuntimeStateRefreshRejectsSnapshotIsolation(t *testing.T) {
	pool := NewPool(t)

	for _, isolation := range []pgx.TxIsoLevel{pgx.RepeatableRead, pgx.Serializable} {
		t.Run(string(isolation), func(t *testing.T) {
			tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: isolation})
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, tx)

			_, err = tx.Exec(t.Context(), `SELECT refresh_youtube_live_runtime_state(ARRAY['any'])`)
			require.ErrorContains(t, err, "live runtime refresh requires read committed transaction")
		})
	}
}
