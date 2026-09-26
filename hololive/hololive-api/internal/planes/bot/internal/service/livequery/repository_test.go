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
 VALUES('CURRENT',1,repeat('a',64),now()+interval '1 hour',now());
 INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until)
 SELECT generation,'UC_live_query','live_snapshot',20,120000,true,valid_until FROM youtube_collection_projection_generations WHERE status='CURRENT';`)

	return &Repository{db: pool}, pool
}

func execFixture(tb testing.TB, pool *pgxpool.Pool, sql string) {
	tb.Helper()

	_, err := pool.Exec(tb.Context(), sql)
	require.NoError(tb, err)
}

func addCoverage(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	execFixture(t, pool, `INSERT INTO youtube_live_absence_slots(observation_id,scheduled_for,evidence_sha256,effective_at,received_at,scope_sha256,coverage)
 VALUES(900001,now()-interval '30 seconds',repeat('b',64),now()-interval '30 seconds',now()-interval '29 seconds',repeat('c',64),
 '{"requested_channel_ids":["UC_live_query"],"filters":{"statuses":["LIVE","UPCOMING"]}}');`)
}

func addLive(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	execFixture(t, pool, `INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,started_at,last_seen_at)
 VALUES('livequery01','UC_live_query','LIVE','Confirmed stream',now()-interval '1 hour',now()+interval '2 days');
 INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at)
 VALUES('livequery01','LIVE',now()-interval '30 seconds',now()-interval '29 seconds');`)
}

func TestRepositoryEvidenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		live   bool
		sql    string
		status Status
		reason Reason
		items  int
	}{
		{name: "consumed complete empty without raw rows", status: Complete, reason: Covered},
		{name: "fresh canonical live", live: true, status: Complete, reason: Covered, items: 1},
		{name: "positive without full coverage", live: true, sql: `DELETE FROM youtube_live_absence_slots`, status: Partial, reason: Incomplete, items: 1},
		{name: "upcoming only is not live coverage", sql: `UPDATE youtube_live_absence_slots SET coverage=jsonb_set(coverage,'{filters,statuses}','["UPCOMING"]')`, status: Unavailable, reason: Incomplete},
		{name: "uncollected", sql: `DELETE FROM youtube_collection_targets`, status: Unavailable, reason: Uncollected},
		{name: "expired projection", sql: `UPDATE youtube_collection_projection_generations SET valid_until=now()-interval '1 second'`, status: Unavailable, reason: InvalidProjection},
		{name: "metadata cannot renew positive", live: true, sql: `UPDATE youtube_live_reconciliation_heads SET last_live_positive_at=now()-interval '6 minutes'`, status: Unavailable, reason: Stale},
		{name: "missing head", live: true, sql: `DELETE FROM youtube_live_reconciliation_heads`, status: Unavailable, reason: Inconsistent},
		{name: "pending head without session", live: true, sql: `DELETE FROM youtube_live_sessions;
 INSERT INTO youtube_live_pending_ends(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('livequery01','UC_live_query','EXPLICIT_END',900002,now(),now(),now(),true,true)`, status: Unavailable, reason: Inconsistent},
		{name: "ended session live head", live: true, sql: `UPDATE youtube_live_sessions SET status='ENDED'`, status: Unavailable, reason: Inconsistent},
		{name: "future positive", live: true, sql: `UPDATE youtube_live_reconciliation_heads SET last_live_positive_at=now()+interval '1 minute'`, status: Unavailable, reason: InvalidClock},
		{name: "future receive", live: true, sql: `UPDATE youtube_live_reconciliation_heads SET last_live_positive_seen_at=now()+interval '1 minute'`, status: Unavailable, reason: InvalidClock},
		{name: "future start", live: true, sql: `UPDATE youtube_live_sessions SET started_at=now()+interval '1 minute'`, status: Unavailable, reason: InvalidClock},
		{name: "nullable actual start", live: true, sql: `UPDATE youtube_live_sessions SET started_at=NULL`, status: Complete, reason: Covered, items: 1},
		{name: "old coverage replay", sql: `UPDATE youtube_live_absence_slots SET effective_at=now()-interval '6 minutes',received_at=now()`, status: Unavailable, reason: Incomplete},
		{name: "future coverage", sql: `UPDATE youtube_live_absence_slots SET effective_at=now()+interval '1 minute'`, status: Unavailable, reason: Incomplete},
		{name: "future coverage receipt", sql: `UPDATE youtube_live_absence_slots SET received_at=now()+interval '1 minute'`, status: Unavailable, reason: Incomplete},
		{name: "old collection slot", sql: `UPDATE youtube_live_absence_slots SET scheduled_for=now()-interval '6 minutes'`, status: Unavailable, reason: Incomplete},
		{name: "pending end even after grace", live: true, sql: `INSERT INTO youtube_live_pending_ends(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('livequery01','UC_live_query','EXPLICIT_END',900002,now(),now(),now(),true,true)`, status: Unavailable, reason: ConfirmingEnd},
		{name: "end before first positive", sql: `INSERT INTO youtube_live_pending_ends(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('earlyquery','UC_live_query','EXPLICIT_END',900003,now(),now(),now(),true,true)`, status: Unavailable, reason: ConfirmingEnd},
	} {
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
			require.False(t, result.AsOf.IsZero())

			if tc.name == "nullable actual start" {
				require.Nil(t, result.Items[0].StartedAt)
			}
		})
	}
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
UPDATE youtube_live_absence_slots SET effective_at=now(),received_at=now(),scheduled_for=now();`)
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

func TestRepositoryScopeSharedChannelAndLimit(t *testing.T) {
	repo, pool := queryFixture(t)
	addCoverage(t, pool)
	addLive(t, pool)
	execFixture(t, pool, `INSERT INTO members(slug,channel_id,english_name,org,sync_source) VALUES
 ('live-query-shared','UC_live_query','Second Name','Hololive','manual'),
 ('live-query-other','UC_other_query','Other Member','VSpo','manual');
 INSERT INTO youtube_collection_targets SELECT projection_generation,'UC_other_query',observation_kind,priority,poll_interval_ms,enabled,valid_until,created_at FROM youtube_collection_targets WHERE subject_key='UC_live_query';
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

	_, err = tx.Exec(t.Context(), "LOCK TABLE youtube_live_absence_slots IN ACCESS EXCLUSIVE MODE")
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
