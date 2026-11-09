package dbtest

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// 별도 invoker 기록 역할이 실제 upgrade 뒤에도 영수증·사실·runtime 판정을 함께 커밋해야 한다.
func TestLiveReviewRecorderDecoderMigrationPreservesRestrictedRecording(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()
	roles := createObservationGrantRoles(t, pool)
	recorder := pgx.Identifier{roles.runtime}.Sanitize()
	_, err := pool.Exec(ctx, `GRANT SELECT,UPDATE ON youtube_live_sessions,youtube_live_reconciliation_heads,
  youtube_live_pending_ends,youtube_video_availability TO `+recorder+`;
  GRANT SELECT,INSERT ON youtube_live_review_receipts,youtube_live_review_receipt_facts TO `+recorder+`;
  GRANT EXECUTE ON FUNCTION record_youtube_live_review(uuid,text,text,text,text),
   youtube_live_review_snapshot(text),youtube_live_review_semantic_facts(jsonb),
   youtube_live_review_receipt_semantic_facts(jsonb),youtube_live_review_receipt_value(jsonb),
   lock_youtube_live_runtime_state(),refresh_youtube_live_runtime_state(text[]) TO `+recorder)
	require.NoError(t, err)
	seedLiveReviewComparison(t, pool)
	RefreshAllLiveRuntimeState(t, pool)
	for _, receiptID := range []string{"50000000-0000-0000-0000-000000000002", "50000000-0000-0000-0000-000000000003"} {
		_, beforeReceipts := liveReviewComparisonState(t, pool)
		assertRestrictedRecorderReopens(t, pool, roles, receiptID, beforeReceipts)
	}
}

func assertRestrictedRecorderReopens(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles, receiptID, beforeReceipts string) {
	t.Helper()

	// 매 재적용 뒤 새로운 사실을 기록하여 기존 영수증 조회만 성공하는 경우를 배제한다.
	ctx := t.Context()
	_, err := pool.Exec(ctx, `UPDATE youtube_live_sessions SET title=$1 WHERE video_id='none'`, receiptID)
	require.NoError(t, err)
	RefreshAllLiveRuntimeState(t, pool)

	var unreviewed bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT reviewed_at IS NULL FROM youtube_live_runtime_state WHERE video_id='none'`).Scan(&unreviewed))
	require.True(t, unreviewed)

	denied := recordLiveReviewAsRole(t, pool, roles.scraper, receiptID)
	pgErr, ok := errors.AsType[*pgconn.PgError](denied)
	require.True(t, ok, "unauthorized recorder must fail with a PostgreSQL error: %v", denied)
	require.Equal(t, "42501", pgErr.Code)

	_, deniedReceipts := liveReviewComparisonState(t, pool)
	require.Equal(t, beforeReceipts, deniedReceipts)
	assertLiveRuntimeState(t, pool)

	require.NoError(t, recordLiveReviewAsRole(t, pool, roles.runtime, receiptID))
	assertLiveReviewReceiptFactsMatchReceipts(t, pool)
	assertLiveRuntimeState(t, pool)

	var receiptAndFacts, runtimeReviewed, unauthorizedRecord, unauthorizedDecoder bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM youtube_live_review_receipts receipt
			JOIN youtube_live_review_receipt_facts facts USING(receipt_id)
			WHERE receipt.receipt_id=$1::uuid AND receipt.video_id='none'
			  AND receipt.operator_id='restricted-recorder' AND receipt.reason='별도 역할 검토'
			  AND receipt.original_snapshot->'session'->>'title'=$1::text
			  AND to_jsonb(facts.semantic_value)=facts.facts),
		EXISTS (SELECT 1 FROM youtube_live_runtime_state state
			JOIN youtube_live_review_receipts receipt ON receipt.video_id=state.video_id
			WHERE receipt.receipt_id=$1::uuid AND state.reviewed_at=receipt.recorded_at),
		has_function_privilege($2,'record_youtube_live_review(uuid,text,text,text,text)','EXECUTE'),
		has_function_privilege($2,'youtube_live_review_receipt_value(jsonb)','EXECUTE')`, receiptID, roles.scraper).
		Scan(&receiptAndFacts, &runtimeReviewed, &unauthorizedRecord, &unauthorizedDecoder))
	require.True(t, receiptAndFacts)
	require.True(t, runtimeReviewed)
	require.False(t, unauthorizedRecord)
	require.False(t, unauthorizedDecoder)
}

func recordLiveReviewAsRole(t *testing.T, pool *pgxpool.Pool, role, receiptID string) error {
	t.Helper()

	var snapshotSHA string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT snapshot_sha256 FROM youtube_live_review_snapshot('none')`).Scan(&snapshotSHA))

	tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(t.Context(), `SET LOCAL ROLE `+pgx.Identifier{role}.Sanitize())
	require.NoError(t, err)

	_, err = tx.Exec(t.Context(), `SELECT record_youtube_live_review($1::uuid,'none',$2,'restricted-recorder','별도 역할 검토')`, receiptID, snapshotSHA)
	if err != nil {
		return err
	}

	return tx.Commit(t.Context())
}

// decoder 외 함수 ACL·invoker 경계·표 ACL·역할 구성은 forward grant 전후에 그대로여야 한다.
