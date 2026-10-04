package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

// fullLiveReviewSnapshotSQL은 가용성 전체 행까지 포함한 현재 원본이다. 기록 CAS가 모든 열을 보호한다.
const fullLiveReviewSnapshotSQL = `
	SELECT jsonb_build_object('session',to_jsonb(session),'head',to_jsonb(head),
	           'pending',to_jsonb(pending),'availability',to_jsonb(availability.*)) AS snapshot
	FROM youtube_live_sessions session
	LEFT JOIN youtube_live_reconciliation_heads head USING (video_id)
	LEFT JOIN youtube_live_pending_ends pending USING (video_id)
	LEFT JOIN youtube_video_availability availability USING (video_id)
	WHERE session.video_id = $1`

const liveReviewMaxSnapshotBytes = 262144

func seedReviewableLegacyVideo(t *testing.T, pool *pgxpool.Pool, videoID string) {
	t.Helper()

	execLiveReviewStatements(t, pool, videoID,
		`INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,lifecycle_origin)
		 VALUES ($1,'fresh','UPCOMING','legacy',now()-interval '2 days','legacy_unknown')`,
		`INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_upcoming_positive_at,last_upcoming_positive_seen_at)
		 VALUES ($1,'UPCOMING',now()-interval '3 days',now()-interval '3 days')`,
		`INSERT INTO youtube_video_availability
		 (video_id,channel_id,provider,identity_confirmed,availability,method,unknown_reason,evidence_sha256,scheduled_for,effective_at,observed_at,received_at)
		 VALUES ($1,'fresh','youtubejs',false,'UNKNOWN','unknown','identity_missing',repeat('a',64),
		         now()-interval '1 day',now()-interval '1 day',now()-interval '1 day',now()-interval '1 day')`,
	)
}

func execLiveReviewStatements(t *testing.T, pool *pgxpool.Pool, videoID string, statements ...string) {
	t.Helper()

	for _, statement := range statements {
		_, err := pool.Exec(t.Context(), statement, videoID)
		require.NoError(t, err)
	}
}

// insertLegacyFormatReceipt는 운영의 기존 27건처럼 246 형식 전체 원본을 다른 시간대 세션에서 기록한다.
func insertLegacyFormatReceipt(t *testing.T, pool *pgxpool.Pool, receiptID, videoID string) {
	t.Helper()

	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackLiveReviewTx(t, tx)

	_, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'Asia/Seoul'`)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, `
		INSERT INTO youtube_live_review_receipts
		(receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason)
		SELECT $2::uuid,$1,encode(sha256(convert_to(full_snapshot.snapshot::text,'UTF8')),'hex'),
		       full_snapshot.snapshot,'{}','closed_unresolved','test-operator','기존 형식 검토'
		FROM (
		    SELECT jsonb_set(full_row.snapshot,'{availability}',full_row.snapshot#>'{availability,availability}') AS snapshot
		    FROM (`+fullLiveReviewSnapshotSQL+`) full_row
		) full_snapshot`, videoID, receiptID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

func recordLiveReview(ctx context.Context, pool *pgxpool.Pool, receiptID, videoID, digest string) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `SELECT record_youtube_live_review($1::uuid,$2,$3,'test-operator','UNKNOWN 검토 종료')`,
		receiptID, videoID, digest); err != nil {
		return errors.Join(err, tx.Rollback(ctx))
	}

	return tx.Commit(ctx)
}

func rollbackLiveReviewTx(t *testing.T, tx pgx.Tx) {
	t.Helper()

	if err := tx.Rollback(t.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Error(err)
	}
}

func liveReviewClosed(ctx context.Context, t *testing.T, pool *pgxpool.Pool, videoID string) bool {
	t.Helper()

	var closed bool

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT reviewed_at IS NOT NULL FROM youtube_live_review_current_receipt($1)`, videoID).Scan(&closed))

	return closed
}

// 기존 형식 영수증은 형식 변환이나 재기록 없이 기술적 갱신 뒤에도 유효하고,
// 일정·가용성·positive 사실이 바뀌면 다시 열린다.
func TestLiveReviewSurvivesTechnicalRefreshAndReopensOnFacts(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, collectionTargetFixture)
	require.NoError(t, err)

	const videoID = "review-legacy"

	seedReviewableLegacyVideo(t, pool, videoID)
	require.False(t, liveReviewClosed(ctx, t, pool, videoID))

	before := observeLifecycleMetrics(t, pool)

	insertLegacyFormatReceipt(t, pool, "30000000-0000-0000-0000-000000000001", videoID)
	require.True(t, liveReviewClosed(ctx, t, pool, videoID))

	// 관측 시각·head 갱신 시각·무시한 부재 slot 증가·같은 판정의 가용성 재확인은 검토를 다시 열지 않는다.
	execLiveReviewStatements(t, pool, videoID,
		`UPDATE youtube_live_sessions SET last_seen_at=now(),status_observed_at=now(),schedule_observed_at=now(),
		        title_observed_at=now(),thumbnail_url='https://example.invalid/refreshed.jpg'
		 WHERE video_id=$1`,
		`UPDATE youtube_live_reconciliation_heads SET updated_at=now(),next_end_check_at=NULL,
		        ignored_absence_scheduled_for=ARRAY(SELECT now()-n*interval '2 minutes' FROM generate_series(1,20000) n)
		 WHERE video_id=$1`,
		`UPDATE youtube_video_availability SET scheduled_for=now()-interval '1 hour',effective_at=now()-interval '1 hour',
		        observed_at=now(),received_at=now(),updated_at=now(),evidence_sha256=repeat('b',64)
		 WHERE video_id=$1`,
	)
	require.True(t, liveReviewClosed(ctx, t, pool, videoID), "technical refresh reopened an unchanged review")

	after := observeLifecycleMetrics(t, pool)
	require.InDelta(t, testutil.ToFloat64(before.lifecycleRecords.WithLabelValues("closed_unresolved"))+1,
		testutil.ToFloat64(after.lifecycleRecords.WithLabelValues("closed_unresolved")), 0)
	require.InDelta(t, testutil.ToFloat64(before.lifecycleRecords.WithLabelValues("unresolved_unreviewed"))-1,
		testutil.ToFloat64(after.lifecycleRecords.WithLabelValues("unresolved_unreviewed")), 0)
	require.InDelta(t, testutil.ToFloat64(before.lifecycleRecords.WithLabelValues("retained_total")),
		testutil.ToFloat64(after.lifecycleRecords.WithLabelValues("retained_total")), 0)

	for _, change := range []struct{ name, apply, revert string }{
		{
			name:   "schedule",
			apply:  `UPDATE youtube_live_sessions SET scheduled_start_time=scheduled_start_time+interval '1 hour' WHERE video_id=$1`,
			revert: `UPDATE youtube_live_sessions SET scheduled_start_time=scheduled_start_time-interval '1 hour' WHERE video_id=$1`,
		},
		{
			name:   "availability",
			apply:  `UPDATE youtube_video_availability SET availability='PUBLIC',method='player_public',unknown_reason=NULL,identity_confirmed=true WHERE video_id=$1`,
			revert: `UPDATE youtube_video_availability SET availability='UNKNOWN',method='unknown',unknown_reason='identity_missing',identity_confirmed=false WHERE video_id=$1`,
		},
		{
			name:   "positive",
			apply:  `UPDATE youtube_live_reconciliation_heads SET last_upcoming_positive_at=last_upcoming_positive_at+interval '1 hour' WHERE video_id=$1`,
			revert: `UPDATE youtube_live_reconciliation_heads SET last_upcoming_positive_at=last_upcoming_positive_at-interval '1 hour' WHERE video_id=$1`,
		},
		{
			name:   "status",
			apply:  `UPDATE youtube_live_sessions SET status='LIVE' WHERE video_id=$1`,
			revert: `UPDATE youtube_live_sessions SET status='UPCOMING' WHERE video_id=$1`,
		},
	} {
		t.Run(change.name, func(t *testing.T) {
			tag, err := pool.Exec(ctx, change.apply, videoID)
			require.NoError(t, err)
			require.EqualValues(t, 1, tag.RowsAffected())

			defer func() {
				_, err := pool.Exec(ctx, change.revert, videoID)
				require.NoError(t, err)
				require.True(t, liveReviewClosed(ctx, t, pool, videoID))
			}()

			require.False(t, liveReviewClosed(ctx, t, pool, videoID))
		})
	}
}

// 무시한 부재 slot이 큰 원본도 요약 snapshot으로 기록할 수 있고, 기록 CAS는 전체 원본을 계속 덮는다.
func TestLiveReviewRecordsOversizedHeadWithBoundedSnapshot(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	const videoID = "review-oversized"

	seedReviewableLegacyVideo(t, pool, videoID)

	_, err := pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads
		SET ignored_absence_scheduled_for=ARRAY(SELECT now()-n*interval '2 minutes' FROM generate_series(1,17006) n)
		WHERE video_id=$1`, videoID)
	require.NoError(t, err)

	var (
		digest, fullDigest     string
		storedBytes, fullBytes int
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT review.snapshot_sha256, octet_length(review.original_snapshot::text),
		       encode(sha256(convert_to(full_snapshot.snapshot::text,'UTF8')),'hex'), octet_length(full_snapshot.snapshot::text)
		FROM youtube_live_review_snapshot($1) review
		CROSS JOIN (`+fullLiveReviewSnapshotSQL+`) full_snapshot`, videoID).Scan(&digest, &storedBytes, &fullDigest, &fullBytes))
	require.Greater(t, fullBytes, liveReviewMaxSnapshotBytes)
	require.Less(t, storedBytes, 16384)
	require.Equal(t, fullDigest, digest, "record CAS must still cover the full current facts")

	// 검토 뒤 부재 slot이 하나라도 늘면 기록 CAS가 거부한다.
	_, err = pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads
		SET ignored_absence_scheduled_for=ignored_absence_scheduled_for||now() WHERE video_id=$1`, videoID)
	require.NoError(t, err)
	require.Error(t, recordLiveReview(ctx, pool, "30000000-0000-0000-0000-000000000002", videoID, digest))
	require.False(t, liveReviewClosed(ctx, t, pool, videoID))

	// 같은 가용성 판정의 재확인도 기록 전에는 전체 원본 CAS를 무효화한다.
	require.NoError(t, pool.QueryRow(ctx, `SELECT snapshot_sha256 FROM youtube_live_review_snapshot($1)`, videoID).Scan(&digest))

	_, err = pool.Exec(ctx, `UPDATE youtube_video_availability SET observed_at=observed_at+interval '1 second' WHERE video_id=$1`, videoID)
	require.NoError(t, err)
	require.Error(t, recordLiveReview(ctx, pool, "30000000-0000-0000-0000-000000000004", videoID, digest))
	require.False(t, liveReviewClosed(ctx, t, pool, videoID))

	require.NoError(t, pool.QueryRow(ctx, `SELECT snapshot_sha256 FROM youtube_live_review_snapshot($1)`, videoID).Scan(&digest))
	require.NoError(t, recordLiveReview(ctx, pool, "30000000-0000-0000-0000-000000000003", videoID, digest))
	require.True(t, liveReviewClosed(ctx, t, pool, videoID))

	var (
		count       int
		hasArray    bool
		summaryHash string
		arrayHash   string
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT (receipt.original_snapshot#>>'{head,ignored_absence_summary,count}')::int,
		       receipt.original_snapshot->'head' ? 'ignored_absence_scheduled_for',
		       receipt.original_snapshot#>>'{head,ignored_absence_summary,sha256}',
		       encode(sha256(convert_to(to_jsonb(head.ignored_absence_scheduled_for)::text,'UTF8')),'hex')
		FROM youtube_live_review_receipts receipt
		JOIN youtube_live_reconciliation_heads head USING (video_id)
		WHERE receipt.receipt_id='30000000-0000-0000-0000-000000000003'`).Scan(&count, &hasArray, &summaryHash, &arrayHash))
	require.Equal(t, 17007, count)
	require.False(t, hasArray)
	require.Equal(t, arrayHash, summaryHash)

	// 이후 부재 slot 증가는 의미 사실이 아니므로 새 형식 영수증도 유지된다.
	_, err = pool.Exec(ctx, `UPDATE youtube_live_reconciliation_heads
		SET ignored_absence_scheduled_for=ignored_absence_scheduled_for||now(), updated_at=now() WHERE video_id=$1`, videoID)
	require.NoError(t, err)
	require.True(t, liveReviewClosed(ctx, t, pool, videoID))
}

// 가용성 확인이 없는 미래 일정 legacy_unknown은 보존·미상 집계에 남지만 지난 미해결 검토 대상은 아니다.
func TestFutureLegacyUpcomingStaysUnknownButNotActionable(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, collectionTargetFixture)
	require.NoError(t, err)

	before := observeLifecycleMetrics(t, pool)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,scheduled_start_time,lifecycle_origin)
		VALUES ('legacy-future','fresh','UPCOMING','',now()+interval '60 days','legacy_unknown'),
		       ('future-conflict','fresh','UPCOMING','',now()+interval '90 days','legacy_unknown'),
		       ('legacy-past-due','fresh','UPCOMING','',now()-interval '1 day','legacy_unknown'),
		       ('legacy-unscheduled','fresh','UPCOMING','',NULL,'legacy_unknown');
		INSERT INTO youtube_live_reconciliation_heads(video_id,status) VALUES ('future-conflict','LIVE')`)
	require.NoError(t, err)

	after := observeLifecycleMetrics(t, pool)
	delta := func(m func(*collectionTargetMetrics) float64) float64 { return m(after) - m(before) }
	record := func(classification string) func(*collectionTargetMetrics) float64 {
		return func(m *collectionTargetMetrics) float64 {
			return testutil.ToFloat64(m.lifecycleRecords.WithLabelValues(classification))
		}
	}

	require.InDelta(t, 4, delta(record("retained_total")), 0)
	require.InDelta(t, 4, delta(record("legacy_unreviewed")), 0)
	require.InDelta(t, 2, delta(record("legacy_not_due")), 0)
	require.InDelta(t, 2, delta(record("unresolved_unreviewed")), 0, "past-due and unscheduled legacy records stay actionable")
	require.InDelta(t, 1, delta(func(m *collectionTargetMetrics) float64 {
		return testutil.ToFloat64(m.liveReview.WithLabelValues("state_mismatch"))
	}), 0, "a future record contradicting its head remains a state mismatch")
}
