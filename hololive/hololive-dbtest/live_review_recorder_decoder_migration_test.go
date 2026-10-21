package dbtest

import (
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"
	"github.com/stretchr/testify/require"
)

const liveReviewRecorderDecoderMigration = "314_youtube_live_review_recorder_decoder_acl.sql"

// 별도 invoker 기록 역할이 실제 upgrade 뒤에도 영수증·사실·runtime 판정을 함께 커밋해야 한다.
func TestLiveReviewRecorderDecoderMigrationPreservesRestrictedRecording(t *testing.T) {
	t.Parallel()

	pool, dir := projectionMigrationPoolBefore(t, liveReviewSharedSourcesMigration)
	ctx := t.Context()
	roles := createObservationGrantRoles(t, pool)
	recorder := pgx.Identifier{roles.runtime}.Sanitize()

	// fixture 역할은 NOINHERIT·NOSUPERUSER이며 운영 runtime의 구성원이 아니다.
	_, err := pool.Exec(ctx, `GRANT SELECT,UPDATE ON youtube_live_sessions,youtube_live_reconciliation_heads,
		youtube_live_pending_ends,youtube_video_availability TO `+recorder+`;
		GRANT INSERT ON youtube_live_review_receipts,youtube_live_review_receipt_facts TO `+recorder+`;
		GRANT EXECUTE ON FUNCTION record_youtube_live_review(uuid,text,text,text,text),
			youtube_live_review_snapshot(text),youtube_live_review_semantic_facts(jsonb),
			youtube_live_review_receipt_semantic_facts(jsonb) TO `+recorder)
	require.NoError(t, err)
	seedLiveReviewComparison(t, pool)

	// 310 이전에도 실제로 기록 가능한 역할이어야 단순 ACL 존재 검사로 회귀를 놓치지 않는다.
	require.NoError(t, recordLiveReviewAsRole(t, pool, roles.runtime, "50000000-0000-0000-0000-000000000001"))
	assertLiveReviewReceiptFactsMatchReceipts(t, pool)

	_, originalReceipts := liveReviewComparisonState(t, pool)

	entries, err := dbmigrate.Manifest(os.DirFS(dir))
	require.NoError(t, err)

	for _, filename := range entries {
		if filename == liveReviewRecorderDecoderMigration {
			break
		}

		require.NoError(t, applyManifestMigration(ctx, pool, dir, filename))
	}

	// 310~313은 아직 별도 기록 역할에 decoder를 주지 않는다.
	var decoderAllowed bool

	require.NoError(t, pool.QueryRow(ctx, `SELECT has_function_privilege($1,
		'youtube_live_review_receipt_value(jsonb)','EXECUTE')`, roles.runtime).Scan(&decoderAllowed))
	require.False(t, decoderAllowed)

	_, upgradedReceipts := liveReviewComparisonState(t, pool)
	require.Equal(t, originalReceipts, upgradedReceipts)

	beforeAuthorization := liveReviewRecorderAuthorizationState(t, pool, roles)

	for i, receiptID := range []string{
		"50000000-0000-0000-0000-000000000002",
		"50000000-0000-0000-0000-000000000003",
	} {
		beforeResults, beforeReceipts := liveReviewComparisonState(t, pool)

		if i == 0 {
			require.NoError(t, applyManifestMigration(ctx, pool, dir, liveReviewRecorderDecoderMigration))
		} else {
			// ledger skip가 아니라 SQL 본문을 다시 적용해 멱등성을 확인한다.
			require.NoError(t, applyMigrationFile(ctx, pool, dir, liveReviewRecorderDecoderMigration))
		}

		afterResults, afterReceipts := liveReviewComparisonState(t, pool)
		require.JSONEq(t, beforeResults, afterResults)
		require.Equal(t, beforeReceipts, afterReceipts)
		require.JSONEq(t, beforeAuthorization, liveReviewRecorderAuthorizationState(t, pool, roles))
		assertLiveRuntimeState(t, pool)

		assertRestrictedRecorderReopens(t, pool, roles, receiptID, afterReceipts)
	}
}

func assertRestrictedRecorderReopens(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles, receiptID, beforeReceipts string) {
	t.Helper()

	// 매 재적용 뒤 새로운 사실을 기록하여 기존 영수증 조회만 성공하는 경우를 배제한다.
	ctx := t.Context()
	_, err := pool.Exec(ctx, `UPDATE youtube_live_sessions SET title=$1 WHERE video_id='none'`, receiptID)
	require.NoError(t, err)

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

	tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
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
func liveReviewRecorderAuthorizationState(t *testing.T, pool *pgxpool.Pool, roles observationGrantRoles) string {
	t.Helper()

	var state string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
		'functions',(SELECT jsonb_agg(jsonb_build_array(oid,proacl,prosecdef) ORDER BY oid)
			FROM pg_proc WHERE pronamespace='public'::regnamespace
			  AND oid<>'youtube_live_review_receipt_value(jsonb)'::regprocedure),
		'tables',(SELECT jsonb_agg(jsonb_build_array(oid,relacl) ORDER BY oid)
			FROM pg_class WHERE relnamespace='public'::regnamespace),
		'roles',(SELECT jsonb_agg(jsonb_build_array(oid,rolsuper,rolinherit) ORDER BY oid)
			FROM pg_roles WHERE rolname IN ($1,$2)),
		'memberships',(SELECT jsonb_agg(to_jsonb(membership) ORDER BY membership.oid)
			FROM pg_auth_members membership JOIN pg_roles role ON role.oid=membership.member
			WHERE role.rolname IN ($1,$2)))::text`, roles.runtime, roles.scraper).Scan(&state))

	return state
}
