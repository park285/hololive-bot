package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/require"
)

const liveReviewReceiptSideFactsSQL = `public.youtube_live_review_semantic_facts(
	CASE WHEN jsonb_typeof(receipt.original_snapshot->'head') = 'object'
	     THEN jsonb_set(receipt.original_snapshot,'{head}',(receipt.original_snapshot->'head') - 'ignored_absence_scheduled_for')
	     ELSE receipt.original_snapshot
	END)`

func TestLiveReviewReceiptFactsStoredOnRecordAndImmutable(t *testing.T) {
	t.Parallel()

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

func TestLiveReviewReceiptFactsPrivilegesOverrideBroadDefaultACL(t *testing.T) {
	t.Parallel()

	pool := NewBlankPool(t)
	ctx := t.Context()
	roles := createObservationGrantRoles(t, pool)
	runtime := pgx.Identifier{roles.runtime}.Sanitize()

	for _, role := range []string{roles.runtime, roles.scraper} {
		_, err := pool.Exec(ctx, "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO "+pgx.Identifier{role}.Sanitize())
		require.NoError(t, err)
	}

	applyBaselineForRoles(t, pool, roles)

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
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
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

	if err := pgxdb.Rollback(t.Context(), tx); err != nil {
		t.Error(err)
	}
}
