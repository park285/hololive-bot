package runtime

import (
	"context"
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	collectionLifecyclePopulation = 30000
	collectionLifecycleReviewed   = 500
)

func TestCollectionLifecycleSnapshotWithinRuntimeBudget(t *testing.T) {
	for _, withReceipts := range []bool{false, true} {
		name := "zero_receipts"

		if withReceipts {
			name = "many_receipts"
		}

		t.Run(name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			seedCollectionLifecyclePopulation(t, pool)

			var reviewed int64

			if withReceipts {
				seedCollectionLifecycleReceipts(t, pool)

				reviewed = collectionLifecycleReviewed
			}

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			started := time.Now()
			rows, err := pool.Query(ctx, mustSQL("collection_target_observability.sql"), defaultLiveFreshnessBudget().Milliseconds())
			require.NoError(t, err)

			defer rows.Close()

			samples, err := scanCollectionTargets(rows)
			require.NoError(t, err)
			t.Logf("snapshot for %d live sessions, reviewed=%d: %s", collectionLifecyclePopulation, reviewed, time.Since(started))
			require.Len(t, samples, collectionTargetJobCount)
			require.EqualValues(t, collectionLifecyclePopulation, samples[0].retainedTotal)
			require.EqualValues(t, collectionLifecyclePopulation, samples[0].upcoming)
			require.Equal(t, collectionLifecyclePopulation-reviewed, samples[0].legacyUnreviewed)
			require.Equal(t, reviewed, samples[0].closedUnresolved)
			require.Zero(t, samples[0].stateMismatch)
		})
	}
}

func TestLiveCheckReviewWorkIsBoundedByReviewedVideos(t *testing.T) {
	for _, withReceipts := range []bool{false, true} {
		name := "zero_receipts"

		if withReceipts {
			name = "many_receipts"
		}

		t.Run(name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			seedCollectionLifecyclePopulation(t, pool)

			var reviewed int

			if withReceipts {
				seedCollectionLifecycleReceipts(t, pool)

				reviewed = collectionLifecycleReviewed
			}

			var raw []byte

			require.NoError(t, pool.QueryRow(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+mustSQL("live_check_videos.sql"),
				[]string{"load-channel"}, int64(270000), 101).Scan(&raw))

			var plans []struct {
				Plan collectionStatePlanNode `json:"Plan"`
			}

			require.NoError(t, jsonv2.Unmarshal(raw, &plans))
			require.Len(t, plans, 1)

			// pending ends는 검토 snapshot만 읽습니다. 영수증 없는 영상과 과거
			// 영수증 세대 수가 현재 snapshot 계산 횟수를 늘리면 안 됩니다.
			lookups := liveReviewPendingEndLookups(plans[0].Plan)
			t.Logf("reviewed videos=%d, pending-end lookups=%.0f", reviewed, lookups)
			require.LessOrEqual(t, lookups, float64(reviewed))
		})
	}
}

func liveReviewPendingEndLookups(node collectionStatePlanNode) float64 {
	var lookups float64

	if node.Relation == "youtube_live_pending_ends" {
		lookups = node.Loops
	}

	for _, child := range node.Plans {
		lookups += liveReviewPendingEndLookups(child)
	}

	return lookups
}

// TestLiveCheckVideosExcludeOnlyMatchedReviews는 현재 snapshot과 일치하는 검토 영수증만 UPCOMING을
// 구조 membership에서 빼고, 검토 뒤 바뀐 영상과 LIVE는 남기는지 확인한다.
func TestLiveCheckVideosExcludeOnlyMatchedReviews(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `
        INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,lifecycle_origin)
        VALUES ('live','review-channel','LIVE','','observed'),
               ('review-current','review-channel','UPCOMING','','legacy_unknown'),
               ('review-changed','review-channel','UPCOMING','','legacy_unknown'),
               ('unreviewed','review-channel','UPCOMING','','legacy_unknown');
        INSERT INTO youtube_live_review_receipts
        (receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason)
        SELECT md5(video_id)::uuid,video_id,snapshot.snapshot_sha256,snapshot.original_snapshot,
               snapshot.evidence_refs,'closed_unresolved','test-operator','현재 snapshot 검토'
        FROM youtube_live_sessions session
        CROSS JOIN LATERAL youtube_live_review_snapshot(session.video_id) snapshot
        WHERE session.video_id IN ('review-current','review-changed');
        UPDATE youtube_live_sessions SET title='changed' WHERE video_id='review-changed';
    `)
	require.NoError(t, err)

	rows, err := pool.Query(ctx, mustSQL("live_check_videos.sql"), []string{"review-channel"}, int64(270000), 10)
	require.NoError(t, err)

	defer rows.Close()

	var ids []string

	for rows.Next() {
		var (
			id, channel string
			upcoming    bool
			notBefore   *time.Time
		)

		require.NoError(t, rows.Scan(&id, &channel, &upcoming, &notBefore))
		require.Equal(t, "review-channel", channel)
		require.Equal(t, id != "live", upcoming)
		require.Nil(t, notBefore, "video without freshness evidence must be immediately eligible")

		ids = append(ids, id)
	}

	require.NoError(t, rows.Err())
	require.Equal(t, []string{"live", "review-changed", "unreviewed"}, ids)
}

// TestLiveCheckOverflowPreservesLastGoodProjection은 구조 membership이 상한을 넘으면 일부를 고정 순서로
// 잘라 쓰지 않고 refresh를 실패시켜 직전 CURRENT를 그대로 유지하는지 실제 Refresh 경로로 확인한다.
func TestLiveCheckOverflowPreservesLastGoodProjection(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	seedLiveCheckProjectionMembers(t, pool)

	_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id, channel_id, status) VALUES ('overflow-0000', 'UC_tp_stale_ops', 'LIVE')`)
	require.NoError(t, err)

	refresher, err := targetprojection.NewRefresher(pool, time.Hour)
	require.NoError(t, err)

	builder := targetprojection.PolicyBuilder{Reader: rosterReader{}, Schedules: targetprojection.DefaultPolicySchedules()}

	lastGood, err := refresher.Refresh(ctx, builder, time.Now())
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
        INSERT INTO youtube_live_sessions(video_id, channel_id, status)
        SELECT 'overflow-' || lpad(n::text, 4, '0'), 'UC_tp_stale_ops', CASE WHEN n % 2 = 0 THEN 'LIVE' ELSE 'UPCOMING' END
        FROM generate_series(1, $1::int) n
    `, targetprojection.MaxInputLiveCheckVideoCount)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE youtube_live_sessions SET scheduled_start_time = statement_timestamp() - INTERVAL '1 hour' WHERE status = 'UPCOMING'`)
	require.NoError(t, err)

	_, err = refresher.Refresh(ctx, builder, time.Now())
	require.ErrorIs(t, err, targetprojection.ErrInputRead)
	require.ErrorIs(t, err, targetprojection.ErrInvalidProjection)

	var (
		current int64
		videos  int
	)

	require.NoError(t, pool.QueryRow(ctx, `
        SELECT g.generation, count(t.subject_key) FILTER (WHERE t.observation_kind = 'video_live_check')
        FROM youtube_collection_projection_generations g
        JOIN youtube_collection_targets t ON t.projection_generation = g.generation
        WHERE g.status = 'CURRENT' AND g.valid_until > statement_timestamp()
        GROUP BY g.generation
    `).Scan(&current, &videos))
	require.Equal(t, lastGood.Generation, current)
	require.Equal(t, 1, videos)
}

func seedCollectionLifecyclePopulation(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
        INSERT INTO youtube_collection_projection_generations
        (status,row_count,projection_sha256,valid_until,activated_at)
        VALUES ('CURRENT',1,repeat('a',64),now()+interval '1 hour',now());
        INSERT INTO youtube_collection_targets
        (projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until)
        SELECT generation,'load-channel','live_snapshot',20,120000,true,now()+interval '1 hour'
        FROM youtube_collection_projection_generations WHERE status='CURRENT';
        INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,lifecycle_origin)
        SELECT 'load-'||n,'load-channel','UPCOMING',repeat('x',500),'legacy_unknown'
        FROM generate_series(1,30000) n;
        INSERT INTO youtube_live_reconciliation_heads(video_id,status)
        SELECT video_id,'UPCOMING' FROM youtube_live_sessions;
        ANALYZE youtube_collection_targets;
        ANALYZE youtube_live_sessions;
        ANALYZE youtube_live_reconciliation_heads;
        ANALYZE youtube_live_review_receipts;
    `)
	require.NoError(t, err)
}

func seedCollectionLifecycleReceipts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
        INSERT INTO youtube_video_availability
        (video_id,channel_id,provider,identity_confirmed,availability,method,unknown_reason,evidence_sha256,
         scheduled_for,effective_at,observed_at,received_at)
        SELECT 'load-'||n,'load-channel','youtubejs',false,'UNKNOWN','unknown','identity_missing',repeat('a',64),
               now(),now(),now(),now() FROM generate_series(1,500) n;
    `)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `
        BEGIN ISOLATION LEVEL SERIALIZABLE;
        SELECT record_youtube_live_review(md5('current-'||n)::uuid,'load-'||n,snapshot.snapshot_sha256,
                   'test-operator','현재 snapshot 검토')
        FROM generate_series(1,500) n
        CROSS JOIN LATERAL youtube_live_review_snapshot('load-'||n) snapshot;
        COMMIT;
        INSERT INTO youtube_live_review_receipts
        (receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason)
        SELECT md5('historical-'||n||'-'||revision)::uuid,'load-'||n,
               encode(sha256(convert_to(jsonb_build_object('historical_revision',revision)::text,'UTF8')),'hex'),
               jsonb_build_object('historical_revision',revision),'{}','closed_unresolved','test-operator','과거 snapshot 검토'
        FROM generate_series(1,500) n CROSS JOIN generate_series(1,30) revision;
        ANALYZE youtube_live_review_receipts;
    `)
	require.NoError(t, err)
}
