package livequery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func queryFixture(tb testing.TB) (*Repository, *pgxpool.Pool) {
	tb.Helper()

	pool := dbtest.NewPool(tb)
	execFixture(tb, pool, `UPDATE members SET is_graduated=true,status='graduated';
 INSERT INTO members(slug,channel_id,english_name,org,sync_source) VALUES('live-query', 'UC_live_query','Query Member','Hololive','manual');
 UPDATE youtube_collection_projection_generations SET status='RETIRED' WHERE status='CURRENT';
 INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
 VALUES('CURRENT',2,repeat('a',64),now()+interval '1 hour',now());
 INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
 SELECT generation,'UC_live_query',kind,20,120000,true FROM youtube_collection_projection_generations
 CROSS JOIN (VALUES('live_snapshot'),('channel_live_check')) kinds(kind) WHERE status='CURRENT';`)

	return &Repository{db: pool}, pool
}

func execFixture(tb testing.TB, pool *pgxpool.Pool, sql string) {
	tb.Helper()

	_, err := pool.Exec(tb.Context(), sql)
	require.NoError(tb, err)
}

func addCoverage(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	execFixture(t, pool, `INSERT INTO youtube_channel_live_checks
 (channel_id,provider,outcome,channel_identity_confirmed,evidence_sha256,scheduled_for,effective_at,observed_at,received_at)
 VALUES('UC_live_query','youtubejs','CHANNEL_PAGE',true,repeat('b',64),now()-interval '30 seconds',
 now()-interval '30 seconds',now()-interval '29 seconds',now()-interval '29 seconds');`)
}

func addLive(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	execFixture(t, pool, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,started_at,last_seen_at)
 VALUES('livequery01','UC_live_query','LIVE','Confirmed stream',now()-interval '1 hour',now()+interval '2 days');
 INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at)
 VALUES('livequery01','LIVE',now()-interval '30 seconds',now()-interval '29 seconds');`)
}

var repositoryEvidenceCases = []struct {
	name        string
	live        bool
	sql         string
	status      Status
	reason      Reason
	items       int
	diagnostics Diagnostics
}{
	{name: "verified channel page without raw rows", status: Complete, reason: Covered},
	{name: "fresh canonical live", live: true, status: Complete, reason: Covered, items: 1},
	{name: "positive without channel confirmation", live: true, sql: `DELETE FROM youtube_channel_live_checks`, status: Partial, reason: Incomplete, items: 1},
	{name: "positive without check target", live: true, sql: `DELETE FROM youtube_collection_targets WHERE observation_kind='channel_live_check'`, status: Partial, reason: Uncollected, items: 1},
	{name: "selected live needs positive confirmation", sql: `UPDATE youtube_channel_live_checks SET outcome='LIVE_VIDEO',selected_video_id='selected01'`, status: Unavailable, reason: Incomplete},
	{name: "unknown replaces a negative", sql: `UPDATE youtube_channel_live_checks SET outcome='UNKNOWN',unknown_reason='request_failed',channel_identity_confirmed=false`, status: Unavailable, reason: Incomplete},
	{name: "absence slot cannot cover a channel", sql: `DELETE FROM youtube_channel_live_checks;
 INSERT INTO youtube_live_absence_slots(observation_id,scheduled_for,evidence_sha256,effective_at,received_at,scope_sha256,coverage)
 VALUES(900001,now(),repeat('b',64),now(),now(),repeat('c',64),'{"requested_channel_ids":["UC_live_query"],"filters":{"statuses":["LIVE"]}}')`, status: Unavailable, reason: Incomplete},
	{name: "uncollected", sql: `DELETE FROM youtube_collection_targets`, status: Unavailable, reason: Uncollected},
	{name: "expired projection", sql: `UPDATE youtube_collection_projection_generations SET valid_until=now()-interval '1 second'`, status: Unavailable, reason: InvalidProjection},
	{name: "metadata cannot renew positive", live: true, sql: `UPDATE youtube_live_reconciliation_heads SET last_live_positive_at=now()-interval '6 minutes'`, status: Unavailable, reason: Stale},
	{name: "missing head", live: true, sql: `DELETE FROM youtube_live_reconciliation_heads`, status: Unavailable, reason: Inconsistent},
	{name: "pending head without session", live: true, sql: `DELETE FROM youtube_live_sessions;
 INSERT INTO youtube_live_pending_ends(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('livequery01','UC_live_query','EXPLICIT_END',900002,now(),now(),now(),true,true)`, status: Unavailable, reason: Inconsistent},
	{name: "ended session live head", live: true, sql: `UPDATE youtube_live_sessions SET status='ENDED'`, status: Complete, reason: Covered, diagnostics: Diagnostics{EndedHeadMismatches: 1}},
	{name: "future positive", live: true, sql: `UPDATE youtube_live_reconciliation_heads SET last_live_positive_at=now()+interval '1 minute'`, status: Unavailable, reason: InvalidClock},
	{name: "future receive", live: true, sql: `UPDATE youtube_live_reconciliation_heads SET last_live_positive_seen_at=now()+interval '1 minute'`, status: Unavailable, reason: InvalidClock},
	{name: "future start", live: true, sql: `UPDATE youtube_live_sessions SET started_at=now()+interval '1 minute'`, status: Unavailable, reason: InvalidClock},
	{name: "nullable actual start", live: true, sql: `UPDATE youtube_live_sessions SET started_at=NULL`, status: Complete, reason: Covered, items: 1},
	{name: "old coverage replay", sql: `UPDATE youtube_channel_live_checks SET effective_at=now()-interval '6 minutes',scheduled_for=now()-interval '6 minutes',received_at=now()`, status: Unavailable, reason: Incomplete},
	{name: "future coverage", sql: `UPDATE youtube_channel_live_checks SET effective_at=now()+interval '1 minute',scheduled_for=now()+interval '1 minute'`, status: Unavailable, reason: Incomplete},
	{name: "future coverage receipt", sql: `UPDATE youtube_channel_live_checks SET received_at=now()+interval '1 minute'`, status: Unavailable, reason: Incomplete},
	{name: "future checked fact", sql: `UPDATE youtube_channel_live_checks SET observed_at=now()+interval '1 minute'`, status: Unavailable, reason: Incomplete},
	{name: "old checked fact", sql: `UPDATE youtube_channel_live_checks SET observed_at=now()-interval '6 minutes'`, status: Unavailable, reason: Incomplete},
	{name: "old receipt", sql: `UPDATE youtube_channel_live_checks SET received_at=now()-interval '6 minutes'`, status: Unavailable, reason: Incomplete},
	{name: "pending end even after grace", live: true, sql: `INSERT INTO youtube_live_pending_ends(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('livequery01','UC_live_query','EXPLICIT_END',900002,now(),now(),now(),true,true)`, status: Unavailable, reason: ConfirmingEnd},
	{name: "end before first positive", sql: `INSERT INTO youtube_live_pending_ends(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('earlyquery','UC_live_query','EXPLICIT_END',900003,now(),now(),now(),true,true)`, status: Complete, reason: Covered, diagnostics: Diagnostics{RetainedOrphanEnds: 1}},
}

func TestRepositoryEvidenceBoundaries(t *testing.T) {
	for _, tc := range repositoryEvidenceCases {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := queryFixture(t)
			addCoverage(t, pool)

			if tc.live {
				addLive(t, pool)
			}

			if tc.sql != "" {
				execFixture(t, pool, tc.sql)
			}

			result, err := repo.Query(t.Context(), Request{Scope: All, Limit: MaxItems})
			require.NoError(t, err)
			require.Equal(t, tc.status, result.Status)
			require.Len(t, result.Channels, 1)
			require.Equal(t, tc.reason, result.Channels[0].Reason)
			require.Len(t, result.Items, tc.items)
			require.Equal(t, tc.diagnostics, result.DiagnosticCounts())
			require.False(t, result.AsOf.IsZero())

			if tc.name == "nullable actual start" {
				require.Nil(t, result.Items[0].StartedAt)
			}
		})
	}
}

func TestRepositoryHeaderExpiryAndHeartbeatWithoutTargetWrites(t *testing.T) {
	repo, pool := queryFixture(t)
	addCoverage(t, pool)
	addLive(t, pool)

	var before, after string

	tuples := `SELECT string_agg(xmin::text||':'||ctid::text,',' ORDER BY subject_key,observation_kind) FROM youtube_collection_targets`
	require.NoError(t, pool.QueryRow(t.Context(), tuples).Scan(&before))
	execFixture(t, pool, `UPDATE youtube_collection_projection_generations SET valid_until=now()-interval '1 second' WHERE status='CURRENT'`)

	expired, err := repo.Query(t.Context(), Request{Scope: All, Limit: MaxItems})
	require.NoError(t, err)
	require.Equal(t, Unavailable, expired.Status)
	require.Equal(t, InvalidProjection, expired.Channels[0].Reason)

	execFixture(t, pool, `UPDATE youtube_collection_projection_generations SET valid_until=now()+interval '1 hour' WHERE status='CURRENT'`)

	renewed, err := repo.Query(t.Context(), Request{Scope: All, Limit: MaxItems})
	require.NoError(t, err)
	require.Equal(t, Complete, renewed.Status)
	require.Len(t, renewed.Items, 1)
	require.NoError(t, pool.QueryRow(t.Context(), tuples).Scan(&after))
	require.Equal(t, before, after, "header renewal must not require target tuple writes")
}

func TestRepositoryReadsCommittedStateAndCoverageTogether(t *testing.T) {
	repo, pool := queryFixture(t)
	addCoverage(t, pool)

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		rollbackErr := tx.Rollback(context.WithoutCancel(t.Context()))
		if !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			require.NoError(t, rollbackErr)
		}
	})

	_, err = tx.Exec(t.Context(), `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title) VALUES('atomicquery','UC_live_query','LIVE','Atomic stream');
INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at) VALUES('atomicquery','LIVE',now(),now());
UPDATE youtube_channel_live_checks SET effective_at=now(),received_at=now(),scheduled_for=now(),observed_at=now();`)
	require.NoError(t, err)

	before, err := repo.Query(t.Context(), Request{Scope: All, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, Complete, before.Status)
	require.Empty(t, before.Items)
	require.NoError(t, tx.Commit(t.Context()))

	after, err := repo.Query(t.Context(), Request{Scope: All, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, Complete, after.Status)
	require.Len(t, after.Items, 1)
	require.NotNil(t, after.Channels[0].CoveredAt)
	require.NotNil(t, before.Channels[0].CoveredAt)
	require.Greater(t, after.Channels[0].CoveredAt.UnixNano(), before.Channels[0].CoveredAt.UnixNano())
}

func TestRepositoryAllScopeUsesMemberDisplayNameOrder(t *testing.T) {
	repo, pool := queryFixture(t)
	addCoverage(t, pool)
	addLive(t, pool)

	for _, tc := range []struct {
		name, shortKorean, korean, want string
	}{
		{name: "short korean name", shortKorean: "쿼리", korean: "쿼리 멤버", want: "쿼리"},
		{name: "korean name", korean: "쿼리 멤버", want: "쿼리 멤버"},
		{name: "english name", want: "Query Member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), `UPDATE members SET short_korean_name=$1, korean_name=$2 WHERE channel_id='UC_live_query'`, tc.shortKorean, tc.korean)
			require.NoError(t, err)

			result, err := repo.Query(t.Context(), Request{Scope: All, Limit: 1})
			require.NoError(t, err)
			require.Len(t, result.Items, 1)
			require.Equal(t, tc.want, result.Items[0].ChannelName)
		})
	}
}

func TestRepositoryScopeSharedChannelAndLimit(t *testing.T) {
	repo, pool := queryFixture(t)
	addCoverage(t, pool)
	addLive(t, pool)
	execFixture(t, pool, `INSERT INTO members(slug,channel_id,english_name,org,sync_source) VALUES
 ('live-query-shared','UC_live_query','Second Name','Hololive','manual'),
 ('live-query-other','UC_other_query','Other Member','VSpo','manual');
 INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,created_at)
 SELECT projection_generation,'UC_other_query',observation_kind,priority,poll_interval_ms,enabled,created_at FROM youtube_collection_targets WHERE subject_key='UC_live_query';
 INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,started_at) VALUES
 ('livequery02','UC_live_query','LIVE','Second stream',now()),('otherquery01','UC_other_query','LIVE','Other stream',now());
 INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at)
 SELECT video_id,'LIVE',now(),now() FROM youtube_live_sessions WHERE video_id IN ('livequery02','otherquery01');`)

	for _, limit := range []int{1, 2, 3} {
		result, err := repo.Query(t.Context(), Request{Scope: All, Limit: limit})
		require.NoError(t, err)
		require.Len(t, result.Channels, 1)
		require.Len(t, result.Items, min(limit, 2))
		require.Equal(t, limit < 2, result.Truncated)
		require.Equal(t, "livequery02", result.Items[0].VideoID)
		require.Equal(t, "Query Member / Second Name", result.Items[0].ChannelName)
	}

	result, err := repo.Query(t.Context(), Request{Scope: Member, ChannelID: "UC_other_query", MemberName: "Resolved Member", Limit: 1})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "otherquery01", result.Items[0].VideoID)
	require.Equal(t, "Resolved Member", result.Items[0].ChannelName)
	require.Equal(t, Partial, result.Status)
	execFixture(t, pool, `UPDATE members SET is_graduated=true,status='graduated' WHERE channel_id='UC_other_query'`)

	result, err = repo.Query(t.Context(), Request{Scope: Member, ChannelID: "UC_other_query", Limit: 1})
	require.NoError(t, err)
	require.Equal(t, Unavailable, result.Status)
	require.Equal(t, InvalidTarget, result.Channels[0].Reason)
}

func TestRepositoryReadOnlyAndCancellation(t *testing.T) {
	repo, pool := queryFixture(t)
	addCoverage(t, pool)

	tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tx.Rollback(context.WithoutCancel(t.Context()))) })

	result, err := (&Repository{db: tx}).Query(t.Context(), Request{Scope: All, Limit: 1})
	require.NoError(t, err)
	require.Equal(t, Complete, result.Status)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = repo.Query(ctx, Request{Scope: All, Limit: 1})
	require.ErrorIs(t, err, context.Canceled)

	_, err = repo.Query(t.Context(), Request{Scope: Member, Limit: 1})
	require.Error(t, err)

	_, err = repo.Query(t.Context(), Request{Scope: All, Limit: 101})
	require.Error(t, err)
}

func TestRepositoryDeadlineIncludesLockWait(t *testing.T) {
	repo, pool := queryFixture(t)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tx.Rollback(context.WithoutCancel(t.Context()))) })

	_, err = tx.Exec(t.Context(), "LOCK TABLE youtube_channel_live_checks IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)

	started := time.Now()

	_, err = repo.Query(t.Context(), Request{Scope: All, Limit: 1})
	require.ErrorIs(t, err, context.DeadlineExceeded, "%v", err)
	require.Less(t, time.Since(started), 3*time.Second)
}

func TestRepositoryDisplayLimitKeepsExtraItemEvidence(t *testing.T) {
	repo, pool := queryFixture(t)
	addCoverage(t, pool)
	execFixture(t, pool, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,started_at)
SELECT 'limit-'||g,'UC_live_query','LIVE','Stream '||g,now() FROM generate_series(1,101) g;
INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at)
SELECT video_id,'LIVE',now(),now() FROM youtube_live_sessions WHERE channel_id='UC_live_query';`)

	result, err := repo.Query(t.Context(), Request{Scope: Member, ChannelID: "UC_live_query", Limit: MaxItems})
	require.NoError(t, err)
	require.Equal(t, Complete, result.Status)
	require.Len(t, result.Items, MaxItems)
	require.True(t, result.Truncated)

	execFixture(t, pool, `DELETE FROM youtube_live_reconciliation_heads WHERE video_id='limit-101';DELETE FROM youtube_live_sessions WHERE video_id='limit-101';`)

	result, err = repo.Query(t.Context(), Request{Scope: Member, ChannelID: "UC_live_query", Limit: MaxItems})
	require.NoError(t, err)
	require.Len(t, result.Items, MaxItems)
	require.False(t, result.Truncated)
}

func TestRepositoryAvailabilityEvidenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sql    string
		status Status
		reason Reason
		items  int
	}{
		{name: "fresh unavailable excludes stale live", status: Complete, reason: Covered},
		{name: "fresh positive wins", sql: `UPDATE youtube_live_reconciliation_heads SET last_live_positive_at=now(),last_live_positive_seen_at=now()`, status: Complete, reason: Covered, items: 1},
		{name: "expired unavailable blocks again", sql: `UPDATE youtube_video_availability SET effective_at=now()-interval '6 minutes',scheduled_for=now()-interval '6 minutes'`, status: Unavailable, reason: Stale},
		{name: "failed recheck blocks again", sql: `UPDATE youtube_video_availability SET availability='UNKNOWN',method='unknown',unknown_reason='request_failed',identity_confirmed=false`, status: Unavailable, reason: Stale},
		{name: "wrong channel cannot exclude", sql: `UPDATE youtube_video_availability SET channel_id='UC_other'`, status: Unavailable, reason: Stale},
		{name: "missing head remains blocking", sql: `DELETE FROM youtube_live_reconciliation_heads`, status: Unavailable, reason: Inconsistent},
		{name: "missing video target cannot exclude", sql: `DELETE FROM youtube_collection_targets WHERE observation_kind='video_live_check'`, status: Unavailable, reason: Stale},
		{name: "stale observed clock cannot exclude", sql: `UPDATE youtube_video_availability SET observed_at=now()-interval '6 minutes'`, status: Unavailable, reason: Stale},
		{name: "future receipt cannot exclude", sql: `UPDATE youtube_video_availability SET received_at=now()+interval '1 minute'`, status: Unavailable, reason: Stale},
		{name: "unavailable pending is retained without blocking", sql: `INSERT INTO youtube_live_pending_ends
 (video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('livequery01','UC_live_query','EXPLICIT_END',900003,now(),now(),now(),true,true)`, status: Complete, reason: Covered},
		{name: "pending identity mismatch still blocks", sql: `INSERT INTO youtube_live_pending_ends
 (video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('livequery01','UC_other','EXPLICIT_END',900003,now(),now(),now(),true,true)`, status: Unavailable, reason: Inconsistent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := queryFixture(t)
			addCoverage(t, pool)
			addLive(t, pool)
			execFixture(t, pool, `UPDATE youtube_live_reconciliation_heads
 SET last_live_positive_at=now()-interval '6 minutes',last_live_positive_seen_at=now()-interval '6 minutes';
 INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
 SELECT generation,'livequery01','video_live_check',20,120000,true FROM youtube_collection_projection_generations WHERE status='CURRENT';
 INSERT INTO youtube_video_availability(video_id,channel_id,provider,identity_confirmed,availability,method,evidence_sha256,scheduled_for,effective_at,observed_at,received_at)
 VALUES('livequery01','UC_live_query','youtubejs',true,'PUBLIC_UNAVAILABLE','player_private',repeat('d',64),
 now()-interval '30 seconds',now()-interval '30 seconds',now()-interval '29 seconds',now()-interval '29 seconds');`)

			if tc.sql != "" {
				execFixture(t, pool, tc.sql)
			}

			for _, request := range []Request{{Scope: All, Limit: MaxItems}, {Scope: Member, ChannelID: "UC_live_query", Limit: MaxItems}} {
				result, err := repo.Query(t.Context(), request)
				require.NoError(t, err)
				require.Equal(t, tc.status, result.Status)
				require.Equal(t, tc.reason, result.Channels[0].Reason)
				require.Len(t, result.Items, tc.items)
			}
		})
	}
}

func seedCrossChannelPending(t *testing.T, pool *pgxpool.Pool, otherOrg, sessionChannel, sessionStatus string) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `INSERT INTO members(slug,channel_id,english_name,org,sync_source)
 VALUES('live-query-other','UC_other_query','Other Member',$1,'manual')`, otherOrg)
	require.NoError(t, err)
	execFixture(t, pool, `INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,created_at)
 SELECT projection_generation,'UC_other_query',observation_kind,priority,poll_interval_ms,enabled,created_at FROM youtube_collection_targets WHERE subject_key='UC_live_query';
 INSERT INTO youtube_channel_live_checks(channel_id,provider,outcome,channel_identity_confirmed,evidence_sha256,scheduled_for,effective_at,observed_at,received_at)
 SELECT 'UC_other_query',provider,outcome,channel_identity_confirmed,evidence_sha256,scheduled_for,effective_at,observed_at,received_at FROM youtube_channel_live_checks WHERE channel_id='UC_live_query'`)

	_, err = pool.Exec(t.Context(), `UPDATE youtube_live_sessions SET channel_id=$1,status=$2 WHERE video_id='livequery01'`, sessionChannel, sessionStatus)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `UPDATE youtube_live_reconciliation_heads SET status=$1 WHERE video_id='livequery01'`, sessionStatus)
	require.NoError(t, err)
	execFixture(t, pool, `INSERT INTO youtube_live_pending_ends
 (video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('livequery01','UC_live_query','EXPLICIT_END',900002,now(),now(),now(),true,true)`)
}
