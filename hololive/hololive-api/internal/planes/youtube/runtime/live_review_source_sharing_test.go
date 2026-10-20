package runtime

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func seedLiveReviewSourceSharing(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_collection_projection_generations
		(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('CURRENT',1,repeat('a',64),now()+interval '1 hour',now());
		INSERT INTO youtube_collection_targets
		(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
		SELECT generation,'fresh','live_snapshot',20,120000,true
		FROM youtube_collection_projection_generations;`)
	require.NoError(t, err)
	seedReviewableLegacyVideo(t, pool, "shared-review")

	return pool
}

// 공유 원본의 NULL·불일치·갱신 판정은 snapshot→semantic_facts의 기존 계약과 같아야 합니다.
type liveReviewSourceCase struct {
	name       string
	before     []string
	after      []string
	wantClosed int64
}

const liveReviewAddPendingSQL = `INSERT INTO youtube_live_pending_ends
	(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
	VALUES ('shared-review','fresh','SCOPED_ABSENCE',1,now()-interval '1 day',now(),now()-interval '1 day',true,true)`

func liveReviewIdentityCases() []liveReviewSourceCase {
	return []liveReviewSourceCase{
		{name: "unchanged", wantClosed: 1},
		{name: "legacy_head_absent", before: []string{`DELETE FROM youtube_live_reconciliation_heads`}, wantClosed: 1},
		{name: "metadata_head_absent", before: []string{
			`DELETE FROM youtube_live_reconciliation_heads`,
			`UPDATE youtube_live_sessions SET lifecycle_origin='metadata_only'`,
		}, wantClosed: 1},
		{name: "observed_head_absent", before: []string{
			`DELETE FROM youtube_live_reconciliation_heads`,
			`UPDATE youtube_live_sessions SET lifecycle_origin='observed'`,
		}},
		{name: "head_live", before: []string{`UPDATE youtube_live_reconciliation_heads SET status='LIVE'`}},
		{name: "head_ended", before: []string{`UPDATE youtube_live_reconciliation_heads SET status='ENDED'`}},
		{name: "ended_session_active_head", before: []string{`UPDATE youtube_live_sessions SET status='ENDED'`}},
		{name: "orphan_active_head", before: []string{
			`INSERT INTO youtube_live_reconciliation_heads(video_id,status) VALUES ('orphan-review','LIVE')`,
		}, wantClosed: 1},
	}
}

func liveReviewFactChangeCases() []liveReviewSourceCase {
	return []liveReviewSourceCase{
		{name: "stale_title", after: []string{`UPDATE youtube_live_sessions SET title='검토 뒤 제목'`}},
		{name: "stale_schedule", after: []string{`UPDATE youtube_live_sessions SET scheduled_start_time=scheduled_start_time+interval '1 hour'`}},
		{name: "stale_positive_timestamp", after: []string{
			`UPDATE youtube_live_reconciliation_heads SET last_upcoming_positive_at=last_upcoming_positive_at+interval '1 microsecond'`,
		}},
		{name: "stale_availability", after: []string{
			`UPDATE youtube_video_availability SET availability='PUBLIC',method='player_public',unknown_reason=NULL,identity_confirmed=true`,
		}},
		{name: "positive_supersedes_availability", after: []string{
			// 의미 사실은 그대로 두고 검토 가능 조건만 바꾸되 가용성의 두 기준 시각은 일치시킵니다.
			`UPDATE youtube_video_availability SET scheduled_for=now()-interval '4 days',effective_at=now()-interval '4 days'`,
		}},
		{name: "availability_absent", before: []string{`DELETE FROM youtube_video_availability`}},
		{name: "pending_added", after: []string{liveReviewAddPendingSQL}},
		{name: "pending_changed", before: []string{liveReviewAddPendingSQL}, after: []string{
			`UPDATE youtube_live_pending_ends SET negative_eligible=false,effective_at=effective_at+interval '1 microsecond'`,
		}},
		{name: "pending_removed", before: []string{liveReviewAddPendingSQL}, after: []string{`DELETE FROM youtube_live_pending_ends`}},
	}
}

func liveReviewTechnicalAndReceiptCases() []liveReviewSourceCase {
	return []liveReviewSourceCase{
		{name: "technical_refresh", before: []string{liveReviewAddPendingSQL}, after: []string{
			`UPDATE youtube_live_sessions SET last_seen_at=now(),title_observed_at=now(),thumbnail_url='https://example.invalid/refreshed.jpg'`,
			`UPDATE youtube_live_reconciliation_heads SET updated_at=now(),
			 ignored_absence_scheduled_for=ARRAY(SELECT now()-n*interval '2 minutes' FROM generate_series(1,20000) n)`,
			`UPDATE youtube_video_availability SET scheduled_for=now()-interval '1 hour',effective_at=now()-interval '1 hour',
			 observed_at=now(),received_at=now(),evidence_sha256=repeat('b',64)`,
			`UPDATE youtube_live_pending_ends SET received_at=received_at+interval '1 hour'`,
		}, wantClosed: 1},
		{name: "duplicate_latest_matching_receipts", after: []string{liveReviewMatchingDuplicateSQL}, wantClosed: 1},
		{name: "latest_receipt_stale_older_matching", after: []string{
			`UPDATE youtube_live_sessions SET title='다른 검토 제목'`,
			liveReviewMatchingDuplicateSQL,
			`UPDATE youtube_live_sessions SET title='legacy'`,
		}, wantClosed: 1},
	}
}

func TestLiveReviewQueriesShareSourcesWithoutSemanticDrift(t *testing.T) {
	pool := seedLiveReviewSourceSharing(t)

	for _, cases := range [][]liveReviewSourceCase{
		liveReviewIdentityCases(), liveReviewFactChangeCases(), liveReviewTechnicalAndReceiptCases(),
	} {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tx, err := pool.Begin(t.Context())
				require.NoError(t, err)

				defer rollbackLiveReviewTx(t, tx)

				for _, statement := range tc.before {
					_, execErr := tx.Exec(t.Context(), statement)
					require.NoError(t, execErr)
				}

				// reviewable이 거짓인 동일 사실 영수증도 둬서 사실 일치만으로 닫히는 오류를 잡습니다.
				_, err = tx.Exec(t.Context(), `
					INSERT INTO youtube_live_review_receipts
					(receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason,recorded_at)
					SELECT '40000000-0000-0000-0000-000000000001','shared-review',snapshot_sha256,original_snapshot,
					       evidence_refs,'closed_unresolved','test-operator','공유 원본 검토',now()-interval '2 days'
					FROM youtube_live_review_snapshot('shared-review')`)
				require.NoError(t, err)

				for _, statement := range tc.after {
					_, execErr := tx.Exec(t.Context(), statement)
					require.NoError(t, execErr)
				}

				dbtest.RefreshAllLiveRuntimeState(t, tx)
				assertLiveReviewQuerySemanticEquivalence(t, tx, []string{"fresh"}, tc.wantClosed)
			})
		}
	}
}

// 같은 영상의 여러 영수증은 한 번만 집계하고 일치하는 영수증 중 최신 시각을 사용합니다.
const liveReviewMatchingDuplicateSQL = `
	UPDATE youtube_live_sessions SET last_seen_at=coalesce(last_seen_at,now())+interval '1 microsecond'
	WHERE video_id='shared-review';
	INSERT INTO youtube_live_review_receipts
	(receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason,recorded_at)
	SELECT '40000000-0000-0000-0000-000000000002','shared-review',snapshot_sha256,original_snapshot,
	       evidence_refs,'closed_unresolved','test-operator','중복 의미 검토',now()-interval '6 hours'
	FROM youtube_live_review_snapshot('shared-review')`

// oracle은 typed helper 대신 기존 전체 snapshot에서 의미 사실을 얻습니다.
// 같은 SQL statement에서 나이까지 비교하므로 실행 시간 허용치나 벽시계 assertion이 필요 없습니다.
const liveReviewSourceOracleSQL = `
	oracle_states AS MATERIALIZED (
	    SELECT session.video_id,session.channel_id,session.status,session.scheduled_start_time,session.lifecycle_origin,
	           head.status AS head_status,
	           head.last_live_positive_at,head.last_live_positive_seen_at,
	           head.last_upcoming_positive_at,head.last_upcoming_positive_seen_at,head.unresolvable_since,
	           availability.effective_at AS availability_at,availability.observed_at AS checked_at,
	           review.reviewed_at
	    FROM youtube_live_sessions session
	    LEFT JOIN youtube_live_reconciliation_heads head USING (video_id)
	    LEFT JOIN youtube_video_availability availability USING (video_id)
	    CROSS JOIN LATERAL (
	        SELECT max(receipt.recorded_at) AS reviewed_at
	        FROM youtube_live_review_snapshot(session.video_id) current
	        JOIN youtube_live_review_receipt_facts receipt ON receipt.video_id=session.video_id
	            AND receipt.facts=youtube_live_review_semantic_facts(current.original_snapshot)
	        WHERE current.reviewable
	    ) review
	    WHERE (session.status IN ('LIVE','UPCOMING') OR head.status IN ('LIVE','UPCOMING'))
	      AND EXISTS (
	          SELECT 1 FROM youtube_collection_targets target
	          JOIN youtube_collection_projection_generations generation ON generation.generation=target.projection_generation
	          WHERE generation.status='CURRENT' AND generation.valid_until>statement_timestamp()
	            AND target.enabled AND target.observation_kind='live_snapshot' AND target.subject_key=session.channel_id)
	)`

func assertLiveReviewQuerySemanticEquivalence(t *testing.T, tx pgx.Tx, channels []string, wantClosed int64) {
	t.Helper()

	var closed int64

	require.NoError(t, tx.QueryRow(t.Context(), `WITH `+liveReviewSourceOracleSQL+`
		SELECT count(*) FILTER (WHERE reviewed_at IS NOT NULL) FROM oracle_states`).Scan(&closed))
	require.Equal(t, wantClosed, closed, "fixture must exercise the intended review state")

	const lifecycleColumns = `live,upcoming,other,state_mismatch,past_due,past_due_7d,retained_total,
		metadata_only,legacy_unreviewed,closed_unresolved,never_checked,oldest_check_age,
		unresolved_unreviewed,legacy_not_due,metadata_never_checked,metadata_check_age,oldest_review_age`

	assertLiveReviewRowsEqual(t, tx, `WITH `+liveReviewSourceOracleSQL+`,
		actual AS (`+strings.TrimSuffix(strings.TrimSpace(mustSQL("collection_target_observability.sql")), ";")+`),
		expected AS (
		    SELECT count(*) FILTER (WHERE head_status='LIVE') AS live,
		           count(*) FILTER (WHERE head_status='UPCOMING') AS upcoming,
		           count(*) FILTER (WHERE head_status IS NULL OR head_status NOT IN ('LIVE','UPCOMING')) AS other,
		           count(*) FILTER (WHERE (head_status IS NOT NULL AND head_status IS DISTINCT FROM status)
		               OR (head_status IS NULL AND (status='LIVE' OR (status='UPCOMING' AND lifecycle_origin='observed')))) AS state_mismatch,
		           count(*) FILTER (WHERE head_status='UPCOMING' AND scheduled_start_time<statement_timestamp()) AS past_due,
		           count(*) FILTER (WHERE head_status='UPCOMING' AND scheduled_start_time<statement_timestamp()-interval '7 days') AS past_due_7d,
		           count(*) AS retained_total,
		           count(*) FILTER (WHERE status='UPCOMING' AND lifecycle_origin='metadata_only') AS metadata_only,
		           count(*) FILTER (WHERE lifecycle_origin='legacy_unknown' AND reviewed_at IS NULL) AS legacy_unreviewed,
		           count(*) FILTER (WHERE reviewed_at IS NOT NULL) AS closed_unresolved,
		           count(*) FILTER (WHERE checked_at IS NULL AND lifecycle_origin='legacy_unknown' AND reviewed_at IS NULL) AS never_checked,
		           coalesce(max(greatest(extract(epoch FROM statement_timestamp()-checked_at),0))
		               FILTER (WHERE lifecycle_origin='legacy_unknown' AND reviewed_at IS NULL),0)::double precision AS oldest_check_age,
		           count(*) FILTER (WHERE reviewed_at IS NULL AND ((lifecycle_origin='legacy_unknown'
		               AND NOT coalesce(status='UPCOMING' AND scheduled_start_time>=statement_timestamp(),false))
		               OR (status='UPCOMING' AND lifecycle_origin='metadata_only' AND scheduled_start_time<statement_timestamp() AND checked_at IS NOT NULL))) AS unresolved_unreviewed,
		           count(*) FILTER (WHERE reviewed_at IS NULL AND lifecycle_origin='legacy_unknown'
		               AND status='UPCOMING' AND scheduled_start_time>=statement_timestamp()) AS legacy_not_due,
		           count(*) FILTER (WHERE lifecycle_origin='metadata_only' AND checked_at IS NULL) AS metadata_never_checked,
		           coalesce(max(greatest(extract(epoch FROM statement_timestamp()-checked_at),0))
		               FILTER (WHERE lifecycle_origin='metadata_only'),0)::double precision AS metadata_check_age,
		           coalesce(max(greatest(extract(epoch FROM statement_timestamp()-reviewed_at),0))
		               FILTER (WHERE reviewed_at IS NOT NULL),0)::double precision AS oldest_review_age
		    FROM oracle_states
		), expected_jobs AS (
		    SELECT kind,`+lifecycleColumns+` FROM (VALUES ('youtubejs_channel_live'),('youtubejs_channel_live_check'),
		        ('youtubejs_video_live'),('community_collect'),('youtubejs_content'),('youtubejs_channel_metadata')) jobs(kind)
		    CROSS JOIN expected
		)
		(SELECT kind,`+lifecycleColumns+` FROM actual EXCEPT ALL SELECT kind,`+lifecycleColumns+` FROM expected_jobs)
		UNION ALL
		(SELECT kind,`+lifecycleColumns+` FROM expected_jobs EXCEPT ALL SELECT kind,`+lifecycleColumns+` FROM actual)`,
		defaultLiveFreshnessBudget().Milliseconds())

	const liveCheckColumns = `video_id,channel_id,is_upcoming,as_of,positive_at,positive_seen_at,
		availability_at,availability_seen_at,unresolvable_since`

	assertLiveReviewRowsEqual(t, tx, `WITH `+liveReviewSourceOracleSQL+`,
		actual AS (`+strings.TrimSuffix(strings.TrimSpace(mustSQL("live_check_videos.sql")), ";")+`),
		expected AS (
		    SELECT video_id,channel_id,status='UPCOMING' AS is_upcoming,statement_timestamp() AS as_of,
		           CASE WHEN status='LIVE' THEN last_live_positive_at ELSE last_upcoming_positive_at END AS positive_at,
		           CASE WHEN status='LIVE' THEN last_live_positive_seen_at ELSE last_upcoming_positive_seen_at END AS positive_seen_at,
		           CASE WHEN status='UPCOMING' THEN availability_at END AS availability_at,
		           CASE WHEN status='UPCOMING' THEN checked_at END AS availability_seen_at,
		           CASE WHEN status='UPCOMING' AND unresolvable_since>coalesce(greatest(last_upcoming_positive_at,last_live_positive_at),'-infinity')
		                THEN unresolvable_since END AS unresolvable_since
		    FROM oracle_states
		    WHERE channel_id=ANY($1::text[]) AND (status='LIVE' OR (status='UPCOMING'
		        AND (scheduled_start_time<statement_timestamp() OR (lifecycle_origin='legacy_unknown' AND scheduled_start_time IS NULL))
		        AND reviewed_at IS NULL))
		    ORDER BY video_id COLLATE "C" LIMIT $2
		)
		(SELECT `+liveCheckColumns+` FROM actual EXCEPT ALL SELECT `+liveCheckColumns+` FROM expected)
		UNION ALL
		(SELECT `+liveCheckColumns+` FROM expected EXCEPT ALL SELECT `+liveCheckColumns+` FROM actual)`, channels, 1000)
}

func assertLiveReviewRowsEqual(t *testing.T, tx pgx.Tx, query string, args ...any) {
	t.Helper()

	rows, err := tx.Query(t.Context(), query, args...)
	require.NoError(t, err)

	defer rows.Close()

	for rows.Next() {
		values, err := rows.Values()
		require.NoError(t, err)
		t.Errorf("runtime query differs from snapshot semantic oracle: %v", values)
	}

	require.NoError(t, rows.Err())
}
