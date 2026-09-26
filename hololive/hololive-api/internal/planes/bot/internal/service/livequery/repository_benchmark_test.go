package livequery

import (
	jsonv2 "encoding/json/v2"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// 명시적인 -bench 실행에서만 장기 이력과 최근 coverage의 비용을 측정한다.
func BenchmarkRepositoryLongHistory(b *testing.B) {
	for _, allLive := range []bool{false, true} {
		name := "sparse-live"

		if allLive {
			name = "all-live"
		}

		b.Run(name, func(b *testing.B) {
			repo, pool := newLongHistoryQueryFixture(b, allLive)
			items := 4

			if allLive {
				items = 74
			}

			benchmarkRepositoryRead(b, repo, pool, Request{Scope: All, Limit: MaxItems}, 74, items)
		})
	}
}

func BenchmarkRepositoryUnobservedPending(b *testing.B) {
	repo, pool := newLongHistoryQueryFixture(b, true)
	execFixture(b, pool, `INSERT INTO youtube_live_pending_ends
 (video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 SELECT 'unobserved-'||g,'channel-'||(g%74),'EXPLICIT_END',g,now()-interval '1 day',now(),now(),true,true
 FROM generate_series(1,50000) g; ANALYZE youtube_live_pending_ends`)

	b.Run("all", func(b *testing.B) {
		benchmarkRepositoryRead(b, repo, pool, Request{Scope: All, Limit: MaxItems}, 74, 74)
	})
	b.Run("member", func(b *testing.B) {
		benchmarkRepositoryRead(b, repo, pool, Request{Scope: Member, ChannelID: "channel-0", Limit: MaxItems}, 1, 1)
	})
}

func benchmarkRepositoryRead(b *testing.B, repo *Repository, pool *pgxpool.Pool, request Request, channels, items int) {
	b.Helper()
	b.ReportAllocs()

	samples := make([]float64, 0)

	beforeWait := pool.Stat().AcquireDuration()

	for b.Loop() {
		started := time.Now()
		result, err := repo.Query(b.Context(), request)

		samples = append(samples, float64(time.Since(started))/float64(time.Millisecond))

		require.NoError(b, err)
		require.Len(b, result.Channels, channels)
		require.Len(b, result.Items, items)
	}

	slices.Sort(samples)
	b.ReportMetric(samples[len(samples)/2], "wall-p50-ms")
	b.ReportMetric(samples[min(len(samples)-1, len(samples)*95/100)], "wall-p95-ms")
	b.ReportMetric(float64(pool.Stat().AcquireDuration()-beforeWait)/float64(time.Microsecond)/float64(len(samples)), "acquire-us/op")

	var raw []byte

	require.NoError(b, pool.QueryRow(b.Context(), "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+snapshotSQL, request.Scope == All, request.ChannelID, request.Limit+1, maxChannels+1).Scan(&raw))

	var plans []struct {
		ExecutionTime float64 `json:"Execution Time"`
		Plan          struct {
			SharedHits  float64 `json:"Shared Hit Blocks"`
			SharedReads float64 `json:"Shared Read Blocks"`
		} `json:"Plan"`
	}

	require.NoError(b, jsonv2.Unmarshal(raw, &plans))
	require.Len(b, plans, 1)

	plan := plans[0].Plan
	b.ReportMetric(plans[0].ExecutionTime, "explain-ms")
	b.ReportMetric(plan.SharedHits, "shared-hits")
	b.ReportMetric(plan.SharedReads, "shared-reads")
	b.Logf("actual plan: %s", raw)
}

func newLongHistoryQueryFixture(b *testing.B, allLive bool) (*Repository, *pgxpool.Pool) {
	b.Helper()

	repo, pool := queryFixture(b)
	execFixture(b, pool, `UPDATE members SET is_graduated=true,status='graduated';
 INSERT INTO members(slug,channel_id,english_name,org,sync_source)
 SELECT 'perf-'||g,'channel-'||g,'Member '||lpad(g::text,3,'0'),'Hololive','manual' FROM generate_series(0,73) g;
 INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until)
 SELECT generation,'channel-'||g,'live_snapshot',20,120000,true,valid_until FROM youtube_collection_projection_generations CROSS JOIN generate_series(0,73) g WHERE status='CURRENT';
 INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,started_at)
 SELECT 'ended-'||g,'channel-'||(g%74),'ENDED','Old stream',now()-interval '2 days' FROM generate_series(1,50000) g;
 INSERT INTO youtube_live_reconciliation_heads(video_id,status) SELECT video_id,'ENDED' FROM youtube_live_sessions WHERE video_id LIKE 'ended-%';
 INSERT INTO youtube_live_pending_ends
 (video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 SELECT video_id,channel_id,'EXPLICIT_END',1,now()-interval '1 day',now(),now(),true,true FROM youtube_live_sessions WHERE video_id LIKE 'ended-%';`)

	_, err := pool.Exec(b.Context(), `INSERT INTO youtube_live_absence_slots
 (observation_id,scheduled_for,evidence_sha256,effective_at,received_at,scope_sha256,coverage)
 SELECT g,now()-interval '10 seconds'-((1500000-g)/119)*interval '2 minutes',repeat('a',64),
 now()-interval '10 seconds'-((1500000-g)/119)*interval '2 minutes',now()-interval '10 seconds'-((1500000-g)/119)*interval '2 minutes',repeat('b',64),
 jsonb_build_object('requested_channel_ids',jsonb_build_array('channel-'||(g%119)),
 'filters',jsonb_build_object('statuses',CASE WHEN $1::bool OR (g%119)%20=0 THEN '["LIVE","UPCOMING"]'::jsonb ELSE '["ENDED"]'::jsonb END)) FROM generate_series(1,1500000) g`, allLive)
	require.NoError(b, err)

	_, err = pool.Exec(b.Context(), `WITH inserted AS (
INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,started_at)
SELECT 'current-'||g,'channel-'||g,'LIVE','Current stream '||g,now()-interval '1 hour'
FROM generate_series(0,73) g WHERE $1::bool OR g%20=0 RETURNING video_id)
INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at)
SELECT video_id,'LIVE',now(),now() FROM inserted`, allLive)
	require.NoError(b, err)
	execFixture(b, pool, `ANALYZE youtube_live_absence_slots;ANALYZE youtube_live_sessions;ANALYZE youtube_live_reconciliation_heads;ANALYZE youtube_live_pending_ends;ANALYZE members;ANALYZE youtube_collection_targets`)

	return repo, pool
}
