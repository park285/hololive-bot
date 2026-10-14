package runtime

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

// 운영 규모의 종료 이력과 영수증이 custom/generic 어느 쪽에서도 활성 집합 비용을 지배하지 않는다.
func TestLiveReviewQueriesBoundedPlans(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedLiveReviewPlanPopulation(t, pool)

	channels := make([]string, 119)
	channelLiterals := make([]string, len(channels))

	for i := range channels {
		channels[i] = fmt.Sprintf("plan-channel-%d", i+1)
		channelLiterals[i] = "'" + channels[i] + "'"
	}

	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer rollbackLiveReviewTx(t, tx)

			prepareLiveReviewPlans(t, tx, mode)

			defer func() {
				_, deallocateErr := tx.Exec(t.Context(), "DEALLOCATE review_observability; DEALLOCATE review_live_check")
				require.NoError(t, deallocateErr)
			}()

			observability := fmt.Sprintf("EXECUTE review_observability(%d)", defaultLiveFreshnessBudget().Milliseconds())
			liveCheck := "EXECUTE review_live_check(ARRAY[" + strings.Join(channelLiterals, ",") + "],1000)"

			assertLiveReviewPlanBlocks(t, tx, observability, 1000)
			assertLiveReviewPlanBlocks(t, tx, liveCheck, 400)

			assertLiveReviewObservability(t, tx, observability)
			assertLiveReviewMembership(t, tx, liveCheck, channels)
		})
	}
}

func prepareLiveReviewPlans(t *testing.T, tx pgx.Tx, mode string) {
	t.Helper()

	_, err := tx.Exec(t.Context(), "SET LOCAL plan_cache_mode = "+mode)
	require.NoError(t, err)

	// SQL PREPARE/EXECUTE로 두 캐시 모드를 실제 매개변수 계획에 적용한다.
	_, err = tx.Exec(t.Context(), "PREPARE review_observability(bigint) AS "+mustSQL("collection_target_observability.sql"))
	require.NoError(t, err)

	_, err = tx.Exec(t.Context(), "PREPARE review_live_check(text[],bigint) AS "+mustSQL("live_check_videos.sql"))
	require.NoError(t, err)
}

func assertLiveReviewObservability(t *testing.T, tx pgx.Tx, query string) {
	t.Helper()

	rows, err := tx.Query(t.Context(), query)
	require.NoError(t, err)

	samples, err := scanCollectionTargets(rows)
	rows.Close()
	require.NoError(t, err)
	require.Len(t, samples, collectionTargetJobCount)

	for i := range samples {
		sample := &samples[i]

		require.EqualValues(t, 215, sample.retainedTotal)
		require.EqualValues(t, 200, sample.upcoming)
		require.EqualValues(t, 15, sample.live)
		require.EqualValues(t, 52, sample.closedUnresolved)
		require.Zero(t, sample.stateMismatch)
	}
}

func assertLiveReviewMembership(t *testing.T, tx pgx.Tx, query string, channels []string) {
	t.Helper()

	rows, err := tx.Query(t.Context(), query)
	require.NoError(t, err)

	var actual []string

	for rows.Next() {
		values, valuesErr := rows.Values()
		require.NoError(t, valuesErr)

		videoID, ok := values[0].(string)
		require.True(t, ok, "live-check video ID must be a string")

		actual = append(actual, videoID)
	}

	require.NoError(t, rows.Err())
	rows.Close()

	// 기존 snapshot→의미 사실 경로를 oracle로 써 새 집합 판정과 membership 전체를 비교한다.
	rows, err = tx.Query(t.Context(), `
		SELECT session.video_id FROM youtube_live_sessions session
		WHERE session.channel_id=ANY($1::text[]) AND (
		    session.status='LIVE' OR (session.status='UPCOMING'
		        AND (session.scheduled_start_time<statement_timestamp()
		             OR (session.lifecycle_origin='legacy_unknown' AND session.scheduled_start_time IS NULL))
		        AND NOT EXISTS (
		            SELECT 1 FROM youtube_live_review_snapshot(session.video_id) current
		            JOIN youtube_live_review_receipt_facts receipt ON receipt.video_id=session.video_id
		                AND receipt.facts=youtube_live_review_semantic_facts(current.original_snapshot)
		            WHERE current.reviewable)))
		ORDER BY session.video_id COLLATE "C"`, channels)
	require.NoError(t, err)

	want, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	require.Len(t, want, 163)
	require.Equal(t, want, actual)
}

func assertLiveReviewPlanBlocks(t *testing.T, tx pgx.Tx, query string, maximum int64) {
	t.Helper()

	var raw []byte

	require.NoError(t, tx.QueryRow(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query).Scan(&raw))

	var plans []struct {
		Plan struct {
			Hits  int64 `json:"Shared Hit Blocks"`
			Reads int64 `json:"Shared Read Blocks"`
		} `json:"Plan"`
	}

	require.NoError(t, jsonv2.Unmarshal(raw, &plans))
	require.Len(t, plans, 1)

	blocks := plans[0].Plan.Hits + plans[0].Plan.Reads
	t.Logf("%s: shared hit+read blocks=%d", query, blocks)
	require.LessOrEqual(t, blocks, maximum, "%s\n%s", query, raw)

	var states []struct {
		Plan collectionStatePlanNode `json:"Plan"`
	}

	require.NoError(t, jsonv2.Unmarshal(raw, &states))
	require.Len(t, states, 1)
	require.Less(t, collectionStateVisits(states[0].Plan), float64(10000), "종료 이력 전체 scan은 허용하지 않습니다: %s", raw)
	assertLiveReviewReceiptScans(t, states[0].Plan)
}

func assertLiveReviewReceiptScans(t *testing.T, node collectionStatePlanNode) {
	t.Helper()

	if node.Relation == "youtube_live_review_receipt_facts" {
		require.LessOrEqual(t, node.Loops, float64(1), "영수증 표는 영상별 상관 loop로 다시 읽지 않아야 합니다")
	}

	for _, child := range node.Plans {
		assertLiveReviewReceiptScans(t, child)
	}
}

func seedLiveReviewPlanPopulation(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_collection_projection_generations
		(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('CURRENT',119,repeat('a',64),now()+interval '1 hour',now());
		INSERT INTO youtube_collection_targets
		(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
		SELECT generation,'plan-channel-'||n,'live_snapshot',20,120000,true
		FROM youtube_collection_projection_generations CROSS JOIN generate_series(1,119) n;
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,lifecycle_origin)
		SELECT 'plan-ended-'||n,'plan-channel-'||(1+n%119),'ENDED',repeat('e',500),'observed'
		FROM generate_series(1,10000) n;
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,lifecycle_origin)
		SELECT 'plan-active-'||n,'plan-channel-'||(1+n%119),CASE WHEN n<=200 THEN 'UPCOMING' ELSE 'LIVE' END,
		       repeat('a',100),now()-interval '2 days',CASE WHEN n<=200 THEN 'legacy_unknown' ELSE 'observed' END
		FROM generate_series(1,215) n;
		INSERT INTO youtube_live_reconciliation_heads(video_id,status)
		SELECT video_id,status FROM youtube_live_sessions WHERE status<>'ENDED';
		INSERT INTO youtube_video_availability
		(video_id,channel_id,provider,identity_confirmed,availability,method,unknown_reason,evidence_sha256,
		 scheduled_for,effective_at,observed_at,received_at)
		SELECT video_id,channel_id,'youtubejs',false,'UNKNOWN','unknown','identity_missing',repeat('a',64),
		       now()-interval '1 day',now()-interval '1 day',now()-interval '1 day',now()-interval '1 day'
		FROM youtube_live_sessions WHERE status='UPCOMING';
		INSERT INTO youtube_live_review_receipts
		(receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason)
		SELECT md5('plan-active-'||n)::uuid,'plan-active-'||n,snapshot.snapshot_sha256,snapshot.original_snapshot,
		       snapshot.evidence_refs,'closed_unresolved','test-operator','집합 검토 계획'
		FROM generate_series(1,53) n
		CROSS JOIN LATERAL youtube_live_review_snapshot('plan-active-'||n) snapshot;
		UPDATE youtube_live_sessions SET title='검토 뒤 변경' WHERE video_id='plan-active-1';
		ANALYZE youtube_collection_projection_generations;
		ANALYZE youtube_collection_targets;
		ANALYZE youtube_collection_job_leases;
		ANALYZE youtube_live_sessions;
		ANALYZE youtube_live_reconciliation_heads;
		ANALYZE youtube_live_pending_ends;
		ANALYZE youtube_video_availability;
		ANALYZE youtube_live_review_receipts;
		ANALYZE youtube_live_review_receipt_facts;
	`)
	require.NoError(t, err)
}
