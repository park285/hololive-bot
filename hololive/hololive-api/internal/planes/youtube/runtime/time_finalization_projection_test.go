package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func finalizationVideos(t *testing.T, pool *pgxpool.Pool, batchSize int) map[string]time.Time {
	t.Helper()

	var videos []targetprojection.TimeFinalizationVideo

	err := dbx.InPgxTx(t.Context(), pool, func(tx dbx.Tx) error {
		var err error

		videos, err = (rosterReader{timeFinalizationBacklogBatchSize: batchSize}).TimeFinalizationVideos(t.Context(), tx)

		return err
	})
	require.NoError(t, err)

	out := make(map[string]time.Time, len(videos))
	for _, video := range videos {
		require.Equal(t, "outside-roster", video.ChannelID)

		out[video.VideoID] = video.NotBefore
	}

	return out
}

func TestTimeFinalizationCandidateEligibility(t *testing.T) {
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `
        INSERT INTO youtube_live_sessions(video_id,channel_id,status,started_at,ended_at,status_observed_at)
        SELECT id,'outside-roster','ENDED',now()-interval '2 hours',now()-interval '1 hour',now()-interval '1 hour'
        FROM unnest(ARRAY['new','positive-only','cancelled','provider','no-start','old','pending-old',
                          'finalized','exhausted','conflict','members_only','reverted']) id;
        UPDATE youtube_live_sessions SET started_at=NULL WHERE video_id IN ('positive-only','no-start');
        UPDATE youtube_live_sessions SET times_source='provider' WHERE video_id='provider';
        UPDATE youtube_live_sessions SET status_observed_at=now()-interval '49 hours'
        WHERE video_id IN ('old','pending-old');
        INSERT INTO youtube_live_reconciliation_heads(video_id,status,end_reason,last_live_positive_at)
        VALUES ('positive-only','ENDED','EXPLICIT_END',now()-interval '2 hours'),
               ('cancelled','ENDED','CANCELLED_BEFORE_LIVE',NULL);
        INSERT INTO youtube_live_time_finalizations(video_id,queue,outcome,retry_base_at,next_attempt_at)
        SELECT video_id,'new','pending',status_observed_at,now()+interval '1 hour'
        FROM youtube_live_sessions WHERE video_id='pending-old';
        INSERT INTO youtube_live_time_finalizations(video_id,queue,outcome,reason,completed_at)
        SELECT video_id,'new',video_id,CASE WHEN video_id='finalized' THEN NULL ELSE 'identity_missing' END,now()
        FROM youtube_live_sessions WHERE video_id IN ('finalized','exhausted','conflict','members_only','reverted');
    `)
	require.NoError(t, err)

	videos := finalizationVideos(t, pool, 0)
	require.Len(t, videos, 3)

	for _, id := range []string{"new", "positive-only", "pending-old"} {
		var expected time.Time

		require.NoError(t, pool.QueryRow(t.Context(), `
            SELECT COALESCE(f.next_attempt_at,s.status_observed_at+interval '3 minutes')
            FROM youtube_live_sessions s LEFT JOIN youtube_live_time_finalizations f USING(video_id)
            WHERE s.video_id=$1`, id).Scan(&expected))
		require.Equal(t, expected, videos[id])
	}
}

func TestTimeFinalizationNewRetriesKeepGeneration(t *testing.T) {
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `
        INSERT INTO youtube_live_sessions(video_id,channel_id,status,started_at,ended_at,status_observed_at)
        VALUES ('new-retry','outside-roster','ENDED',now()-interval '2 hours',now()-interval '1 hour',now()-interval '1 hour');
    `)
	require.NoError(t, err)

	refresher, err := targetprojection.NewRefresher(pool, time.Hour)
	require.NoError(t, err)

	builder := targetprojection.PolicyBuilder{Reader: rosterReader{}, Schedules: targetprojection.DefaultPolicySchedules()}
	first, err := refresher.Refresh(t.Context(), builder, time.Now())
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `
        INSERT INTO youtube_live_time_finalizations(video_id,queue,outcome,attempts,retry_base_at,next_attempt_at)
        SELECT video_id,'new','pending',1,status_observed_at,now()+interval '30 minutes'
        FROM youtube_live_sessions WHERE video_id='new-retry';
    `)
	require.NoError(t, err)

	for attempt := 1; attempt <= 3; attempt++ {
		_, err = pool.Exec(t.Context(), `UPDATE youtube_live_time_finalizations
            SET attempts=$1::smallint,next_attempt_at=now()+$1::smallint*interval '1 hour' WHERE video_id='new-retry'`, attempt)
		require.NoError(t, err)

		next, refreshErr := refresher.Refresh(t.Context(), builder, time.Now())
		require.NoError(t, refreshErr)
		require.Equal(t, first.Generation, next.Generation)
		require.Equal(t, first.SHA256, next.SHA256)

		var sameDeadline, sameMembership bool

		require.NoError(t, pool.QueryRow(t.Context(), `
            SELECT t.not_before=f.next_attempt_at,t.member_since_generation=$1
            FROM youtube_collection_targets t JOIN youtube_live_time_finalizations f ON f.video_id=t.subject_key
            WHERE t.projection_generation=$1 AND t.subject_key='new-retry'`, first.Generation).
			Scan(&sameDeadline, &sameMembership))
		require.True(t, sameDeadline)
		require.True(t, sameMembership)
	}
}

func seedFinalizationBacklog(t *testing.T, pool *pgxpool.Pool, count int) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
        INSERT INTO youtube_live_sessions(video_id,channel_id,status,started_at,ended_at,status_observed_at)
        SELECT 'backlog-'||n,'outside-roster','ENDED',now()-interval '10 days',
               now()-interval '5 days'-n*interval '1 minute',now()-interval '5 days'
        FROM generate_series(1,$1::int) n`, count)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `
        INSERT INTO youtube_live_time_finalizations(video_id,queue,outcome,observed_started_at,observed_ended_at,enqueued_at)
        SELECT video_id,'backlog','pending',started_at,ended_at,now()-interval '1 minute'
        FROM youtube_live_sessions WHERE channel_id='outside-roster'`)
	require.NoError(t, err)
}

// backlogWaiting은 차례를 기다리는 backlog 행의 NotBefore다.
func backlogWaiting(t *testing.T, pool *pgxpool.Pool, videoID string) time.Time {
	t.Helper()

	var waiting time.Time

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT enqueued_at+interval '365 days'
        FROM youtube_live_time_finalizations WHERE video_id=$1`, videoID).Scan(&waiting))

	return waiting
}

// lastBacklogTurn은 마지막 backlog 결과(없으면 투입) 6초 뒤, 즉 다음 첫 시도 시각이다.
func lastBacklogTurn(t *testing.T, pool *pgxpool.Pool, videoID string) time.Time {
	t.Helper()

	var turn time.Time

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT GREATEST(
            (SELECT max(last_attempt_at) FROM youtube_live_time_finalizations WHERE queue='backlog'),
            (SELECT enqueued_at FROM youtube_live_time_finalizations WHERE video_id=$1)) + interval '6 seconds'`,
		videoID).Scan(&turn))

	return turn
}

func TestTimeFinalizationBacklogRotatesOnlyAfterFirstResults(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedFinalizationBacklog(t, pool, 4)
	require.Empty(t, finalizationVideos(t, pool, 0))

	// 첫 시도는 한 영상만 열고, 나머지는 membership만 유지한다.
	firstVideos := finalizationVideos(t, pool, 2)
	require.Len(t, firstVideos, 2)
	require.Equal(t, lastBacklogTurn(t, pool, "backlog-1"), firstVideos["backlog-1"])
	require.Equal(t, backlogWaiting(t, pool, "backlog-2"), firstVideos["backlog-2"])

	refresher, err := targetprojection.NewRefresher(pool, time.Hour)
	require.NoError(t, err)

	builder := targetprojection.PolicyBuilder{
		Reader:    rosterReader{timeFinalizationBacklogBatchSize: 2},
		Schedules: targetprojection.DefaultPolicySchedules(),
	}
	first, err := refresher.Refresh(t.Context(), builder, time.Now())
	require.NoError(t, err)

	// 확정으로 세션 출처가 바뀌어도 활성 묶음의 나머지가 첫 결과를 낼 때까지 남고, 차례는 다음 영상으로 넘어간다.
	_, err = pool.Exec(t.Context(), `
        UPDATE youtube_live_time_finalizations SET outcome='finalized',completed_at=now(),retry_base_at=now(),
            last_attempt_at=now()
        WHERE video_id='backlog-1';
        UPDATE youtube_live_sessions SET times_source='provider' WHERE video_id='backlog-1';
    `)
	require.NoError(t, err)

	partial, err := refresher.Refresh(t.Context(), builder, time.Now())
	require.NoError(t, err)
	require.Equal(t, first.Generation, partial.Generation)
	require.Equal(t, first.SHA256, partial.SHA256)

	partialVideos := finalizationVideos(t, pool, 2)
	require.Len(t, partialVideos, 2)
	require.Equal(t, lastBacklogTurn(t, pool, "backlog-2"), partialVideos["backlog-2"])

	var sleeping time.Time

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT completed_at+interval '30 days'
        FROM youtube_live_time_finalizations WHERE video_id='backlog-1'`).Scan(&sleeping))
	require.Equal(t, sleeping, partialVideos["backlog-1"])

	_, err = pool.Exec(t.Context(), `UPDATE youtube_live_time_finalizations
        SET attempts=1,retry_base_at=now(),last_attempt_at=now(),next_attempt_at=now()+interval '30 minutes'
        WHERE video_id='backlog-2'`)
	require.NoError(t, err)

	rotated, err := refresher.Refresh(t.Context(), builder, time.Now())
	require.NoError(t, err)
	require.Greater(t, rotated.Generation, first.Generation)
	require.NotEqual(t, first.SHA256, rotated.SHA256)

	rotatedVideos := finalizationVideos(t, pool, 2)
	require.Len(t, rotatedVideos, 3)
	require.NotContains(t, rotatedVideos, "backlog-1")
	require.Equal(t, lastBacklogTurn(t, pool, "backlog-3"), rotatedVideos["backlog-3"])
	require.Equal(t, backlogWaiting(t, pool, "backlog-4"), rotatedVideos["backlog-4"])

	assertBacklogRetryMembership(t, pool, refresher, builder, rotated.Generation)
}

func assertBacklogRetryMembership(t *testing.T, pool *pgxpool.Pool, refresher *targetprojection.Refresher,
	builder targetprojection.PolicyBuilder, generation int64,
) {
	t.Helper()

	unchanged, err := refresher.Refresh(t.Context(), builder, time.Now())
	require.NoError(t, err)
	require.Equal(t, generation, unchanged.Generation)

	// 앞 묶음의 pending 재시도는 시각만 바꾸고, 최종 결과를 받으면 대상에서 빠진다.
	_, err = pool.Exec(t.Context(), `UPDATE youtube_live_time_finalizations SET attempts=2,
        next_attempt_at=now()+interval '3 hours' WHERE video_id='backlog-2'`)
	require.NoError(t, err)

	retried, err := refresher.Refresh(t.Context(), builder, time.Now())
	require.NoError(t, err)
	require.Equal(t, generation, retried.Generation)

	_, err = pool.Exec(t.Context(), `UPDATE youtube_live_time_finalizations
        SET outcome='exhausted',reason='identity_missing',completed_at=now(),next_attempt_at=NULL
        WHERE video_id='backlog-2'`)
	require.NoError(t, err)
	require.Len(t, finalizationVideos(t, pool, 2), 2)
	require.Empty(t, finalizationVideos(t, pool, 0))
}

// 재시도 중인 행이 예산을 채우면 다음 묶음을 열지 않고, 최종 결과로 자리가 나면 다음 첫 시도 하나만 연다.
func TestTimeFinalizationBacklogOpensNextBatchOnlyWithinBudget(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedFinalizationBacklog(t, pool, 2002)

	_, err := pool.Exec(t.Context(), `UPDATE youtube_live_time_finalizations
        SET attempts=1,retry_base_at=now()-interval '1 hour',last_attempt_at=now()-interval '1 hour',
            reason='identity_missing',next_attempt_at=now()+interval '2 hours'
        WHERE video_id IN (SELECT 'backlog-'||n FROM generate_series(1,2000) n)`)
	require.NoError(t, err)

	blocked := finalizationVideos(t, pool, 2)
	require.Len(t, blocked, 2000)
	require.NotContains(t, blocked, "backlog-2001")
	require.NotContains(t, blocked, "backlog-2002")

	_, err = pool.Exec(t.Context(), `UPDATE youtube_live_time_finalizations
        SET outcome='exhausted',completed_at=now(),last_attempt_at=now(),next_attempt_at=NULL
        WHERE video_id IN ('backlog-1','backlog-2')`)
	require.NoError(t, err)

	opened := finalizationVideos(t, pool, 2)
	require.Len(t, opened, 2000)
	require.Equal(t, lastBacklogTurn(t, pool, "backlog-2001"), opened["backlog-2001"])
	require.WithinDuration(t, time.Now().Add(6*time.Second), opened["backlog-2001"], time.Minute)
	require.Equal(t, backlogWaiting(t, pool, "backlog-2002"), opened["backlog-2002"])
}

func TestTimeFinalizationReaderPopulationWithinRuntimeBudget(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedFinalizationBacklog(t, pool, 10000)

	_, err := pool.Exec(t.Context(), `
        INSERT INTO youtube_live_sessions(video_id,channel_id,status,started_at,ended_at,status_observed_at)
        SELECT 'new-'||n,'outside-roster','ENDED',now()-interval '2 hours',now()-interval '1 hour',
               now()-n*interval '1 second' FROM generate_series(1,350) n;
        ANALYZE youtube_live_sessions;
        ANALYZE youtube_live_time_finalizations;
    `)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)

	defer cancel()

	started := time.Now()

	var videos []targetprojection.TimeFinalizationVideo

	err = dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		var readErr error

		videos, readErr = (rosterReader{timeFinalizationBacklogBatchSize: 200}).TimeFinalizationVideos(ctx, tx)

		return readErr
	})
	require.NoError(t, err)
	t.Logf("finalization targets for 10350 sessions: %s", time.Since(started))
	require.Len(t, videos, 500)

	ids := make(map[string]bool, len(videos))
	for _, video := range videos {
		ids[video.VideoID] = true
	}

	require.True(t, ids["new-1"])
	require.True(t, ids["new-300"])
	require.False(t, ids["new-301"])
	require.True(t, ids["backlog-200"])
	require.False(t, ids["backlog-201"])
}

func TestTimeFinalizationBacklogWithoutUnattemptedRowsKeepsOnlyPending(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedFinalizationBacklog(t, pool, 4)

	_, err := pool.Exec(t.Context(), `
        UPDATE youtube_live_time_finalizations
        SET deferrals=1,retry_base_at=now(),next_attempt_at=now()+interval '10 minutes';
        UPDATE youtube_live_time_finalizations
        SET outcome='members_only',completed_at=now(),next_attempt_at=NULL WHERE video_id='backlog-2';
        UPDATE youtube_live_sessions SET times_source='provider' WHERE video_id='backlog-3';
        UPDATE youtube_live_sessions SET started_at=NULL WHERE video_id='backlog-4';
    `)
	require.NoError(t, err)

	videos := finalizationVideos(t, pool, 2)
	require.Len(t, videos, 1)
	require.Contains(t, videos, "backlog-1")
}
