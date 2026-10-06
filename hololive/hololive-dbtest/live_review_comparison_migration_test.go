package dbtest

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestLiveReviewComparisonMigrationPreservesReceiptsAndReplays(t *testing.T) {
	const migration = "270_youtube_live_review_compact_comparison.sql"

	pool, dir := projectionMigrationPoolBefore(t, migration)
	seedLiveReviewComparison(t, pool)

	beforeResults, beforeReceipts := liveReviewComparisonState(t, pool)

	for range 2 {
		require.NoError(t, applyMigrationFile(t.Context(), pool, dir, migration))

		afterResults, afterReceipts := liveReviewComparisonState(t, pool)
		require.JSONEq(t, beforeResults, afterResults)
		require.Equal(t, beforeReceipts, afterReceipts, "migration must not rewrite receipts")
	}

	var matched, unmatched, latest bool

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT (SELECT reviewed_at IS NOT NULL FROM youtube_live_review_current_receipt('array')),
		       (SELECT reviewed_at IS NULL FROM youtube_live_review_current_receipt('none')),
		       (SELECT reviewed_at='2026-10-01T02:00:00Z'::timestamptz FROM youtube_live_review_current_receipt('array'))
	`).Scan(&matched, &unmatched, &latest))
	require.True(t, matched)
	require.True(t, unmatched)
	require.True(t, latest, "newer nonmatching receipt must not replace the latest matching receipt")
}

func seedLiveReviewComparison(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() {
		if rollbackErr := tx.Rollback(t.Context()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Error(rollbackErr)
		}
	}()

	_, err = tx.Exec(t.Context(), `
		SET LOCAL TIME ZONE 'Asia/Seoul';
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,lifecycle_origin)
		SELECT video_id,'review-channel','UPCOMING','legacy','2026-09-01T00:00:00Z','legacy_unknown'
		FROM unnest(ARRAY['array','headless','none']) video_id;
		INSERT INTO youtube_live_reconciliation_heads(video_id,status,ignored_absence_scheduled_for)
		VALUES ('array','UPCOMING',ARRAY(SELECT '2026-09-01T00:00:00Z'::timestamptz+n*interval '2 minutes' FROM generate_series(1,4000) n));
		INSERT INTO youtube_video_availability
		(video_id,channel_id,provider,identity_confirmed,availability,method,unknown_reason,evidence_sha256,scheduled_for,effective_at,observed_at,received_at)
		SELECT video_id,channel_id,'youtubejs',false,'UNKNOWN','unknown','identity_missing',repeat('a',64),
		       '2026-09-05T00:00:00Z','2026-09-05T00:00:00Z','2026-09-05T00:00:00Z','2026-09-05T00:00:00Z'
		FROM youtube_live_sessions;
		INSERT INTO youtube_live_review_receipts
		(receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason,recorded_at)
		SELECT gen_random_uuid(),session.video_id,repeat(n::text,64),
		       jsonb_build_object('session',to_jsonb(session),'head',to_jsonb(head),
		           'pending',NULL,'availability',to_jsonb(availability.availability)),
		       '{}','closed_unresolved','test-operator','비교 재현','2026-10-01T00:00:00Z'::timestamptz+n*interval '1 hour'
		FROM youtube_live_sessions session
		LEFT JOIN youtube_live_reconciliation_heads head USING(video_id)
		JOIN youtube_video_availability availability USING(video_id)
		CROSS JOIN generate_series(1,2) n WHERE session.video_id<>'none';
		INSERT INTO youtube_live_review_receipts
		(receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason,recorded_at)
		SELECT gen_random_uuid(),video_id,repeat('f',64),
		       jsonb_set(original_snapshot,'{session,title}','"different"'),
		       '{}','closed_unresolved','test-operator','다른 사실','2026-10-01T03:00:00Z'
		FROM youtube_live_review_receipts WHERE snapshot_sha256=repeat('2',64);
	`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
}

func liveReviewComparisonState(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()

	var results, receipts string

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT jsonb_object_agg(video_id,reviewed_at)::text
		FROM youtube_live_sessions session
		CROSS JOIN LATERAL youtube_live_review_current_receipt(session.video_id) review`).Scan(&results))
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT jsonb_agg(to_jsonb(receipt) ORDER BY receipt_id)::text FROM youtube_live_review_receipts receipt`).Scan(&receipts))

	return results, receipts
}
