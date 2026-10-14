package dbtest

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const liveReviewSetMigration = "304_youtube_live_review_set_comparison.sql"

// 기존 판정·최신 일치 시각·원본 영수증은 재적용과 집합 호출에서도 그대로다.
func TestLiveReviewSetMigrationPreservesDecisions(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveReviewSetMigration)
	seedLiveReviewComparison(t, pool)
	seedRecordedLiveReviews(t, pool)
	seedLiveReviewTypedFacts(t, pool)

	beforeResults, beforeReceipts := liveReviewComparisonState(t, pool)

	for range 2 {
		require.NoError(t, applyMigrationFile(t.Context(), pool, dir, liveReviewSetMigration))

		afterResults, afterReceipts := liveReviewComparisonState(t, pool)
		require.JSONEq(t, beforeResults, afterResults)
		require.Equal(t, beforeReceipts, afterReceipts)
		assertLiveReviewReceiptFactsMatchReceipts(t, pool)

		var setResults string

		require.NoError(t, pool.QueryRow(t.Context(), `
			WITH reviewed AS MATERIALIZED (
			    SELECT video_id, reviewed_at FROM youtube_live_review_current_receipts(ARRAY(SELECT video_id FROM youtube_live_sessions))
			)
			SELECT jsonb_object_agg(session.video_id,reviewed.reviewed_at)::text
			FROM youtube_live_sessions session LEFT JOIN reviewed USING(video_id)`).Scan(&setResults))
		require.JSONEq(t, beforeResults, setResults)
	}

	var typedDecisions string

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT jsonb_object_agg(session.video_id,review.reviewed_at IS NOT NULL)::text
		FROM youtube_live_sessions session
		CROSS JOIN LATERAL youtube_live_review_current_receipt(session.video_id) review
		WHERE session.channel_id='typed-channel'`).Scan(&typedDecisions))
	require.JSONEq(t, `{"typed-full":true,"typed-null":true,"typed-noavail":false,"typed-observed":false,
		"typed-mismatch":false,"typed-live":false,"typed-ended":false,"typed-positive":false,"typed-equal":false}`, typedDecisions)

	for _, zone := range []string{"UTC", "Asia/Seoul", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			assertLiveReviewTypedFactsForZone(t, pool, zone)
		})
	}

	var (
		emptyCount, missingCount int
		missingReviewed          bool
	)

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT (SELECT count(*) FROM youtube_live_review_current_receipts('{}'::text[])),
		       (SELECT count(*) FROM youtube_live_review_current_receipts(ARRAY['missing',NULL,'missing'])),
		       (SELECT reviewed_at IS NULL FROM youtube_live_review_current_receipt('missing'))
	`).Scan(&emptyCount, &missingCount, &missingReviewed))
	require.Zero(t, emptyCount)
	require.Zero(t, missingCount)
	require.True(t, missingReviewed)

	// 1µs 차이는 기술적 갱신이 아니라 일정 사실 변경이다.
	_, err := pool.Exec(t.Context(), `UPDATE youtube_live_sessions SET scheduled_start_time=scheduled_start_time+interval '1 microsecond' WHERE video_id='typed-full'`)
	require.NoError(t, err)

	var reopened bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT reviewed_at IS NULL FROM youtube_live_review_current_receipt('typed-full')`).Scan(&reopened))
	require.True(t, reopened)
}

func assertLiveReviewTypedFactsForZone(t *testing.T, pool *pgxpool.Pool, zone string) {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(t.Context(), `SELECT set_config('TimeZone',$1,true)`, zone)
	require.NoError(t, err)

	var (
		compared int
		equal    bool
	)

	// 전체 원본 JSON 경로는 최적화 함수와 독립적인 의미 사실 oracle이다.
	require.NoError(t, tx.QueryRow(t.Context(), `
		SELECT count(current.video_id),bool_and(current.facts = youtube_live_review_semantic_facts(
		    jsonb_build_object('session',to_jsonb(session),'head',to_jsonb(head),
		        'pending',to_jsonb(pending),'availability',to_jsonb(availability.availability)))
		    AND current.reviewable IS NOT DISTINCT FROM (
		        session.status='UPCOMING'
		        AND (head.video_id IS NULL OR head.status=session.status)
		        AND (session.lifecycle_origin<>'observed' OR head.video_id IS NOT NULL)
		        AND availability.video_id IS NOT NULL
		        AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)
		            >= availability.effective_at,false)))
		FROM youtube_live_review_current_facts(ARRAY(SELECT video_id FROM youtube_live_sessions)) current
		JOIN youtube_live_sessions session USING(video_id)
		LEFT JOIN youtube_live_reconciliation_heads head USING(video_id)
		LEFT JOIN youtube_live_pending_ends pending USING(video_id)
		LEFT JOIN youtube_video_availability availability USING(video_id)`).Scan(&compared, &equal))
	require.GreaterOrEqual(t, compared, 14)
	require.True(t, equal, "typed 사실은 NULL 행·NULL 열·시간대·소수초를 보존해야 합니다")
}

func seedLiveReviewTypedFacts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,lifecycle_origin)
		SELECT video_id,'typed-channel','UPCOMING','legacy_unknown'
		FROM unnest(ARRAY['typed-full','typed-null','typed-noavail','typed-observed','typed-mismatch',
		                 'typed-live','typed-ended','typed-positive','typed-equal']) video_id;
		UPDATE youtube_live_sessions SET
		    title='소수초 사실',topic_id='topic',is_premiere=true,
		    scheduled_start_time='2026-09-01T00:00:00.123456Z',started_at='2026-09-01T01:00:00.234567Z',
		    ended_at='2026-09-01T02:00:00.345678Z',live_first_seen_at='2026-09-01T03:00:00.456789Z'
		WHERE video_id='typed-full';
		UPDATE youtube_live_sessions SET lifecycle_origin='observed' WHERE video_id='typed-observed';
		UPDATE youtube_live_sessions SET status='LIVE' WHERE video_id='typed-live';
		UPDATE youtube_live_sessions SET status='ENDED' WHERE video_id='typed-ended';
		INSERT INTO youtube_live_pending_ends
		(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,ended_at,negative_eligible,scope_covers)
		VALUES ('typed-full','typed-channel','EXPLICIT_END',9001,'2026-09-02T00:00:00.111111Z',now(),
		        '2026-09-02T01:00:00.222222Z','2026-09-02T02:00:00.333333Z',false,true);
		INSERT INTO youtube_live_reconciliation_heads(video_id,status) VALUES
		('typed-full','UPCOMING'),('typed-null','UPCOMING'),('typed-mismatch','LIVE'),
		('typed-positive','UPCOMING'),('typed-equal','UPCOMING');
		UPDATE youtube_live_reconciliation_heads SET
		    last_upcoming_positive_at='2026-09-01T00:00:00.111111Z',
		    last_upcoming_positive_seen_at='2026-09-01T01:00:00.222222Z',
		    last_live_positive_at='2026-09-01T02:00:00.333333Z',
		    last_live_positive_seen_at='2026-09-01T03:00:00.444444Z',
		    last_end_evidence_at='2026-09-01T04:00:00.555555Z',
		    last_complete_absence_at='2026-09-01T05:00:00.666666Z',
		    last_absence_scheduled_for='2026-09-01T06:00:00.777777Z',
		    first_absence_scheduled_for='2026-09-01T07:00:00.888888Z',
		    second_absence_scheduled_for='2026-09-01T08:00:00.999999Z',
		    ended_at='2026-09-01T09:00:00.012345Z',end_reason='EXPLICIT_END',
		    consecutive_absence_slots=3,last_absence_observation_id=9000,
		    end_candidate_kind='EXPLICIT_END',end_candidate_observation_id=9001,next_end_check_at=now()
		WHERE video_id='typed-full';
		INSERT INTO youtube_video_availability
		(video_id,channel_id,provider,identity_confirmed,availability,method,unknown_reason,evidence_sha256,
		 scheduled_for,effective_at,observed_at,received_at)
		SELECT video_id,channel_id,'youtubejs',false,'UNKNOWN','unknown','identity_missing',repeat('a',64),
		       '2026-09-05T00:00:00.123456Z','2026-09-05T00:00:00.123456Z',now(),now()
		FROM youtube_live_sessions WHERE channel_id='typed-channel' AND video_id<>'typed-noavail';
		UPDATE youtube_live_reconciliation_heads SET last_upcoming_positive_at='2026-09-05T00:00:00.123457Z'
		WHERE video_id='typed-positive';
		UPDATE youtube_live_reconciliation_heads SET last_live_positive_at='2026-09-05T00:00:00.123456Z'
		WHERE video_id='typed-equal';
		INSERT INTO youtube_live_review_receipts
		(receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason)
		SELECT md5(session.video_id)::uuid,session.video_id,repeat('a',64),
		       jsonb_strip_nulls(jsonb_build_object('session',to_jsonb(session),'head',to_jsonb(head),
		           'pending',to_jsonb(pending),'availability',to_jsonb(availability.availability))),
		       '{}','closed_unresolved','test-operator','NULL 누락과 소수초 비교'
		FROM youtube_live_sessions session
		LEFT JOIN youtube_live_reconciliation_heads head USING(video_id)
		LEFT JOIN youtube_live_pending_ends pending USING(video_id)
		LEFT JOIN youtube_video_availability availability USING(video_id)
		WHERE session.channel_id='typed-channel';
	`)
	require.NoError(t, err)
}

// 새 집합 함수도 runtime 조회 권한만으로 실행되며 PUBLIC/scraper에는 열리지 않는다.
func TestLiveReviewSetMigrationPrivileges(t *testing.T) {
	pool, dir := projectionMigrationPoolBefore(t, liveReviewSetMigration)
	roles := createObservationGrantRoles(t, pool)
	runtime := pgx.Identifier{roles.runtime}.Sanitize()

	_, err := pool.Exec(t.Context(), `GRANT SELECT ON youtube_live_sessions,youtube_live_reconciliation_heads,
		youtube_live_pending_ends,youtube_video_availability,youtube_live_review_receipt_facts TO `+runtime)
	require.NoError(t, err)

	raw, err := fs.ReadFile(os.DirFS(dir), liveReviewSetMigration)
	require.NoError(t, err)

	migration := strings.NewReplacer("hololive_runtime", roles.runtime, "hololive_scraper", roles.scraper).Replace(string(raw))
	require.NoError(t, applyMigrationContent(t.Context(), pool, liveReviewSetMigration, migration))
	seedLiveReviewComparison(t, pool)

	for _, function := range []string{"youtube_live_review_current_facts(text[])", "youtube_live_review_current_receipts(text[])", "youtube_live_review_current_receipt(text)"} {
		var runtimeAllowed, scraperAllowed bool

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT has_function_privilege($1,$3,'EXECUTE'),has_function_privilege($2,$3,'EXECUTE')`,
			roles.runtime, roles.scraper, function).Scan(&runtimeAllowed, &scraperAllowed))
		require.True(t, runtimeAllowed, function)
		require.False(t, scraperAllowed, function)
	}

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackLiveReviewReceiptFactsTx(t, tx)

	_, err = tx.Exec(t.Context(), "SET LOCAL ROLE "+runtime)
	require.NoError(t, err)

	var reviewed int

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT count(*) FROM youtube_live_review_current_receipts(ARRAY['array','headless','array',NULL])`).Scan(&reviewed))
	require.Equal(t, 2, reviewed)
}
