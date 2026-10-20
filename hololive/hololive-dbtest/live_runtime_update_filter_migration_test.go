package dbtest

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

const liveRuntimeUpdateFilterMigration = "316_youtube_live_runtime_update_filter.sql"

func TestLiveRuntimeUpdateFilterMigrationPreservesDataAndPrivileges(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveRuntimeUpdateFilterMigration)
	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)

	beforeState, beforeReceipts := liveReviewComparisonState(t, pool)
	beforeSecurity := liveRuntimeRefreshSecurity(t, pool)

	for range 2 {
		require.NoError(t, applyMigrationFile(t.Context(), pool, dir, liveRuntimeUpdateFilterMigration))
		assertLiveRuntimeState(t, pool)

		afterState, afterReceipts := liveReviewComparisonState(t, pool)
		require.JSONEq(t, beforeState, afterState)
		require.Equal(t, beforeReceipts, afterReceipts)
		require.JSONEq(t, beforeSecurity, liveRuntimeRefreshSecurity(t, pool))
	}
}

func liveRuntimeRefreshSecurity(t *testing.T, db LiveRuntimeStateReader) string {
	t.Helper()

	var security string

	require.NoError(t, db.QueryRow(t.Context(), `SELECT jsonb_build_array(proowner::regrole::text,proacl::text,prosecdef,provolatile,proconfig)::text
 FROM pg_proc WHERE oid='public.refresh_youtube_live_runtime_state_statement()'::regprocedure`).Scan(&security))

	return security
}

func TestLiveRuntimeUpdateFilterInvalidatesCandidateReceiptFields(t *testing.T) {
	pool := NewPool(t)
	seedRecordedLiveReviews(t, pool)
	RefreshAllLiveRuntimeState(t, pool)

	for _, scenario := range []struct{ name, change, restore string }{
		{"kind", `UPDATE youtube_live_reconciliation_heads SET end_candidate_kind='EXPLICIT_END' WHERE video_id='recorded'`, `UPDATE youtube_live_reconciliation_heads SET end_candidate_kind='SCOPED_ABSENCE' WHERE video_id='recorded'`},
		// 실제 head 선행 저장 순서의 deferred FK 중간 상태를 검사한다.
		{"observation", `UPDATE youtube_live_reconciliation_heads SET end_candidate_observation_id=8 WHERE video_id='recorded'`, `UPDATE youtube_live_reconciliation_heads SET end_candidate_observation_id=7 WHERE video_id='recorded'`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, tx)

			reviewedAt := seedCandidateFieldReceipt(t, tx)
			tag, err := tx.Exec(t.Context(), scenario.change)
			require.NoError(t, err)
			require.Equal(t, int64(1), tag.RowsAffected())
			refreshRecordedLiveRuntimeState(t, tx)
			assertLiveRuntimeState(t, tx)
			require.Nil(t, candidateFieldReviewedAt(t, tx))

			tag, err = tx.Exec(t.Context(), scenario.restore)
			require.NoError(t, err)
			require.Equal(t, int64(1), tag.RowsAffected())
			refreshRecordedLiveRuntimeState(t, tx)
			assertLiveRuntimeState(t, tx)
			require.Equal(t, reviewedAt, candidateFieldReviewedAt(t, tx))
			require.NoError(t, pgxdb.Rollback(t.Context(), tx))
			assertLiveRuntimeState(t, pool)
		})
	}
}

func seedCandidateFieldReceipt(t *testing.T, tx pgx.Tx) *time.Time {
	t.Helper()

	_, err := tx.Exec(t.Context(), `UPDATE youtube_live_reconciliation_heads
 SET end_candidate_kind='SCOPED_ABSENCE',end_candidate_observation_id=7,next_end_check_at='2026-09-04'
 WHERE video_id='recorded'`)
	require.NoError(t, err)
	refreshRecordedLiveRuntimeState(t, tx)
	assertLiveRuntimeState(t, tx)

	_, err = tx.Exec(t.Context(), `SELECT record_youtube_live_review(gen_random_uuid(),'recorded',snapshot.snapshot_sha256,'test-operator','후보 입력 검증')
 FROM youtube_live_review_snapshot('recorded') snapshot`)
	require.NoError(t, err)
	assertLiveRuntimeState(t, tx)

	reviewedAt := candidateFieldReviewedAt(t, tx)
	require.NotNil(t, reviewedAt)

	return reviewedAt
}

func refreshRecordedLiveRuntimeState(t *testing.T, tx pgx.Tx) {
	t.Helper()

	_, err := tx.Exec(t.Context(), `SELECT refresh_youtube_live_runtime_state(ARRAY['recorded'])`)
	require.NoError(t, err)
}

func candidateFieldReviewedAt(t *testing.T, db LiveRuntimeStateReader) *time.Time {
	t.Helper()

	var reviewedAt *time.Time

	require.NoError(t, db.QueryRow(t.Context(), `SELECT reviewed_at FROM youtube_live_runtime_state WHERE video_id='recorded'`).Scan(&reviewedAt))

	return reviewedAt
}
