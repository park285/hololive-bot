package dbtest

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const liveReviewReceiptFactsMigration = "279_youtube_live_review_receipt_facts.sql"

// liveReviewReceiptSideFactsSQL은 270 youtube_live_review_current_receipt가 호출마다 영수증에서 다시 계산하던 식이다.
const liveReviewReceiptSideFactsSQL = `public.youtube_live_review_semantic_facts(
	CASE WHEN jsonb_typeof(receipt.original_snapshot->'head') = 'object'
	     THEN jsonb_set(receipt.original_snapshot,'{head}',(receipt.original_snapshot->'head') - 'ignored_absence_scheduled_for')
	     ELSE receipt.original_snapshot
	END)`

// 기존 246·262 형식 영수증의 판정은 저장 사실로 바꾼 뒤에도 같고, 저장 사실은 270 영수증 쪽 식과 같다.
func TestLiveReviewReceiptFactsMigrationPreservesDecisions(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveReviewReceiptFactsMigration)
	ctx := t.Context()

	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)

	beforeResults, beforeReceipts := liveReviewComparisonState(t, pool)

	for range 2 {
		require.NoError(t, applyMigrationFile(ctx, pool, dir, liveReviewReceiptFactsMigration))

		afterResults, afterReceipts := liveReviewComparisonState(t, pool)
		require.JSONEq(t, beforeResults, afterResults)
		require.Equal(t, beforeReceipts, afterReceipts, "migration must not rewrite receipts")
		assertLiveReviewReceiptFactsMatchReceipts(t, pool)
	}

	var legacyLatest, recorded, reopened, unreviewed bool

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT (SELECT reviewed_at='2026-10-01T02:00:00Z'::timestamptz FROM youtube_live_review_current_receipt('array')),
		       (SELECT reviewed_at IS NOT NULL FROM youtube_live_review_current_receipt('recorded')),
		       (SELECT reviewed_at IS NULL FROM youtube_live_review_current_receipt('reopened')),
		       (SELECT reviewed_at IS NULL FROM youtube_live_review_current_receipt('none'))
	`).Scan(&legacyLatest, &recorded, &reopened, &unreviewed))
	require.True(t, legacyLatest, "246 형식 영수증의 최신 일치 기록 시각이 유지되어야 합니다")
	require.True(t, recorded, "262 형식 영수증이 현재 사실과 일치해야 합니다")
	require.True(t, reopened, "검토 뒤 의미 사실이 바뀐 영상은 다시 열려야 합니다")
	require.True(t, unreviewed)

	// 영수증 쪽 판정 입력은 저장 사실뿐이다. 의미 사실 정의를 바꾸고 다시 채우지 않은 상태처럼 비우면
	// 면제가 모두 풀리는 안전한 방향이고, 같은 backfill을 다시 적용하면 같은 판정으로 돌아온다.
	_, err := pool.Exec(ctx, `TRUNCATE youtube_live_review_receipt_facts`)
	require.NoError(t, err)

	var stillReviewed int

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(review.reviewed_at)
		FROM youtube_live_sessions session
		CROSS JOIN LATERAL youtube_live_review_current_receipt(session.video_id) review`).Scan(&stillReviewed))
	require.Zero(t, stillReviewed, "저장 사실이 없으면 어떤 영수증도 현재 원본을 면제하지 않아야 합니다")

	require.NoError(t, applyMigrationFile(ctx, pool, dir, liveReviewReceiptFactsMigration))

	refilledResults, _ := liveReviewComparisonState(t, pool)
	require.JSONEq(t, beforeResults, refilledResults)
	assertLiveReviewReceiptFactsMatchReceipts(t, pool)
}

// 기록 함수와 직접 INSERT 모두 같은 트랜잭션에서 사실을 저장하고, 저장 사실은 영수증처럼 변경·삭제를 거부한다.
func TestLiveReviewReceiptFactsStoredOnRecordAndImmutable(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)
	assertLiveReviewReceiptFactsMatchReceipts(t, pool)

	var recorded bool

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT reviewed_at IS NOT NULL FROM youtube_live_review_current_receipt('recorded')`).Scan(&recorded))
	require.True(t, recorded)

	_, err := pool.Exec(ctx, `UPDATE youtube_live_review_receipt_facts SET facts='{}' WHERE video_id='recorded'`)
	require.ErrorContains(t, err, "append-only")

	_, err = pool.Exec(ctx, `DELETE FROM youtube_live_review_receipt_facts WHERE video_id='recorded'`)
	require.ErrorContains(t, err, "append-only")

	assertLiveReviewReceiptFactsMatchReceipts(t, pool)
}

// 기본 ACL이 새 표에 줄 수 있는 넓은 권한을 걷어 runtime은 조회만, scraper는 권한 없음으로 남긴다.
func TestLiveReviewReceiptFactsPrivilegesOverrideBroadDefaultACL(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveReviewReceiptFactsMigration)
	ctx := t.Context()
	roles := createObservationGrantRoles(t, pool)
	runtime := pgx.Identifier{roles.runtime}.Sanitize()
	scraper := pgx.Identifier{roles.scraper}.Sanitize()

	// 운영 runtime이 이미 가진 판정 입력 조회·실행 권한과, 운영 기본 ACL(init-db 기준 runtime CRUD)보다 넓은 상위 집합을 재현합니다.
	for _, statement := range []string{
		`GRANT SELECT ON youtube_live_sessions, youtube_live_reconciliation_heads, youtube_live_pending_ends,
		       youtube_video_availability, youtube_live_review_receipts TO ` + runtime,
		`GRANT EXECUTE ON FUNCTION youtube_live_review_semantic_facts(jsonb), youtube_live_review_current_receipt(text) TO ` + runtime,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO " + runtime,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO " + scraper,
	} {
		_, err := pool.Exec(ctx, statement)
		require.NoError(t, err)
	}

	raw, err := fs.ReadFile(os.DirFS(dir), liveReviewReceiptFactsMigration)
	require.NoError(t, err)

	migration := strings.NewReplacer("hololive_runtime", roles.runtime, "hololive_scraper", roles.scraper).Replace(string(raw))

	for range 2 {
		require.NoError(t, applyMigrationContent(ctx, pool, liveReviewReceiptFactsMigration, migration))
	}

	for _, check := range []struct {
		role       string
		wantSelect bool
	}{
		{roles.runtime, true},
		{roles.scraper, false},
	} {
		var canSelect, canWrite bool

		require.NoError(t, pool.QueryRow(ctx, `
			SELECT has_table_privilege($1,'public.youtube_live_review_receipt_facts','SELECT'),
			       has_table_privilege($1,'public.youtube_live_review_receipt_facts',
			           'INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER')`,
			check.role).Scan(&canSelect, &canWrite))
		require.Equal(t, check.wantSelect, canSelect, check.role)
		require.False(t, canWrite, check.role)
	}

	// scraper에는 개별 권한이 없으므로 PUBLIC 실행 권한이 남았는지도 함께 드러난다.
	for _, function := range []string{
		"youtube_live_review_receipt_semantic_facts(jsonb)",
		"store_youtube_live_review_receipt_facts()",
		"youtube_live_review_current_receipt(text)",
	} {
		var executable bool

		require.NoError(t, pool.QueryRow(ctx,
			`SELECT has_function_privilege($1,$2,'EXECUTE')`, roles.scraper, function).Scan(&executable))
		require.False(t, executable, function)
	}

	seedLiveReviewComparison(t, pool)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(ctx, "SET LOCAL ROLE "+runtime)
	require.NoError(t, err)

	var reviewed bool

	require.NoError(t, tx.QueryRow(ctx,
		`SELECT reviewed_at IS NOT NULL FROM youtube_live_review_current_receipt('array')`).Scan(&reviewed))
	require.True(t, reviewed, "runtime은 저장 사실을 읽어 기존 판정을 계산해야 합니다")
}

// seedRecordedLiveReviews는 기록 함수로 262 형식(무시한 부재 slot 요약·가용성 전체 행) 영수증을 남긴다.
// 서울 시간대 세션에서 기록하고 pending end를 포함해 시간대·pending 사실도 저장 사실 비교에 넣는다.
// 기록 뒤 제목이 바뀐 reopened는 더 이상 면제되지 않는다.
func seedRecordedLiveReviews(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,lifecycle_origin)
		SELECT video_id,'review-channel','UPCOMING','recorded','2026-09-01T00:00:00Z','legacy_unknown'
		FROM unnest(ARRAY['recorded','reopened']) video_id;
		INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_upcoming_positive_at,ignored_absence_scheduled_for)
		SELECT video_id,'UPCOMING','2026-09-02T00:00:00Z',
		       ARRAY(SELECT '2026-09-01T00:00:00Z'::timestamptz+n*interval '2 minutes' FROM generate_series(1,4000) n)
		FROM unnest(ARRAY['recorded','reopened']) video_id;
		INSERT INTO youtube_live_pending_ends
		(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
		VALUES ('recorded','review-channel','SCOPED_ABSENCE',7,'2026-09-03T00:00:00Z','2026-09-03T00:00:00Z',
		        '2026-09-03T00:00:00Z',true,true);
		INSERT INTO youtube_video_availability
		(video_id,channel_id,provider,identity_confirmed,availability,method,unknown_reason,evidence_sha256,scheduled_for,effective_at,observed_at,received_at)
		SELECT video_id,channel_id,'youtubejs',false,'UNKNOWN','unknown','identity_missing',repeat('a',64),
		       '2026-09-05T00:00:00Z','2026-09-05T00:00:00Z','2026-09-05T00:00:00Z','2026-09-05T00:00:00Z'
		FROM youtube_live_sessions WHERE video_id IN ('recorded','reopened')`)
	require.NoError(t, err)

	recordLiveReviewInSeoul(t, pool, "40000000-0000-0000-0000-000000000001", "recorded")
	recordLiveReviewInSeoul(t, pool, "40000000-0000-0000-0000-000000000002", "reopened")

	_, err = pool.Exec(ctx, `UPDATE youtube_live_sessions SET title='changed after review' WHERE video_id='reopened'`)
	require.NoError(t, err)
}

func recordLiveReviewInSeoul(t *testing.T, pool *pgxpool.Pool, receiptID, videoID string) {
	t.Helper()

	ctx := t.Context()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'Asia/Seoul'`)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		SELECT record_youtube_live_review($1::uuid,$2,snapshot.snapshot_sha256,'test-operator','저장 사실 재현')
		FROM youtube_live_review_snapshot($2) snapshot`, receiptID, videoID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

// assertLiveReviewReceiptFactsMatchReceipts는 영수증마다 저장 사실이 하나 있고 270 영수증 쪽 식과 같은지 확인한다.
// 비교 세션은 UTC라 서울 시간대에서 기록한 사실이 시간대와 무관한지도 함께 확인한다.
func assertLiveReviewReceiptFactsMatchReceipts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	var receipts, matched int

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(receipt.receipt_id),
		       count(stored.receipt_id) FILTER (WHERE stored.video_id = receipt.video_id
		           AND stored.recorded_at = receipt.recorded_at
		           AND stored.facts = `+liveReviewReceiptSideFactsSQL+`)
		FROM youtube_live_review_receipts receipt
		LEFT JOIN youtube_live_review_receipt_facts stored USING (receipt_id)`).Scan(&receipts, &matched))
	require.Positive(t, receipts)
	require.Equal(t, receipts, matched, "every receipt needs stored facts equal to the 270 receipt-side expression")
}

func rollbackLiveReviewReceiptFactsTx(t *testing.T, tx pgx.Tx) {
	t.Helper()

	if err := tx.Rollback(t.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Error(err)
	}
}
