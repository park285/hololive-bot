package runtime

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestTimeFinalizationMetricsReflectQueuesAndRecentDelays(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedFinalizationBacklog(t, pool, 4)

	_, err := pool.Exec(t.Context(), `
        UPDATE youtube_live_time_finalizations SET outcome='finalized',completed_at=now(),
            retry_base_at=now()-interval '20 minutes' WHERE video_id='backlog-1';
        UPDATE youtube_live_time_finalizations SET outcome='exhausted',reason='identity_missing',completed_at=now()
            WHERE video_id='backlog-2';
        INSERT INTO youtube_live_sessions(video_id,channel_id,status,started_at,status_observed_at)
        SELECT id,'outside-roster','ENDED',now()-interval '3 days',now()-interval '50 minutes'
        FROM unnest(ARRAY['new-final','new-old-final','new-empty','new-pending','old-empty','no-start']) id;
        UPDATE youtube_live_sessions SET status_observed_at=now()-interval '3 days'
            WHERE video_id IN ('new-pending','old-empty','new-old-final');
        UPDATE youtube_live_sessions SET started_at=NULL WHERE video_id='no-start';
        INSERT INTO youtube_live_time_finalizations(video_id,queue,outcome,completed_at)
        VALUES ('new-final','new','finalized',now()),('new-old-final','new','finalized',now()-interval '25 hours');
        INSERT INTO youtube_live_time_finalizations(video_id,queue,outcome,next_attempt_at)
        VALUES ('new-pending','new','pending',now()+interval '3 hours');
    `)
	require.NoError(t, err)

	reg := prometheus.NewPedanticRegistry()
	metrics := newCollectionTargetMetrics(reg)
	r := &Runtime{pool: pool}
	samples, err := r.loadTimeFinalizationSamples(t.Context())
	require.NoError(t, err)
	metrics.timeFinalization.observe(samples)

	m := metrics.timeFinalization
	require.InDelta(t, 2, testutil.ToFloat64(m.queue.WithLabelValues("new")), 0)
	require.InDelta(t, 2, testutil.ToFloat64(m.queue.WithLabelValues("backlog")), 0)
	require.InDelta(t, 3, testutil.ToFloat64(m.outcomes.WithLabelValues("pending", "")), 0)
	require.InDelta(t, 3, testutil.ToFloat64(m.outcomes.WithLabelValues("finalized", "")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.outcomes.WithLabelValues("exhausted", "identity_missing")), 0)
	require.Zero(t, testutil.ToFloat64(m.delay.WithLabelValues("new", "1800")))
	require.InDelta(t, 1, testutil.ToFloat64(m.delay.WithLabelValues("new", "3600")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.delay.WithLabelValues("new", "+Inf")), 0)
	require.Zero(t, testutil.ToFloat64(m.delay.WithLabelValues("backlog", "180")))
	require.InDelta(t, 1, testutil.ToFloat64(m.delay.WithLabelValues("backlog", "1800")), 0)
	require.InDelta(t, 1, testutil.ToFloat64(m.delay.WithLabelValues("backlog", "+Inf")), 0)

	_, err = reg.Gather()
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `UPDATE youtube_live_time_finalizations SET outcome='reverted',reason=NULL
        WHERE video_id='backlog-2'`)
	require.NoError(t, err)

	samples, err = r.loadTimeFinalizationSamples(t.Context())
	require.NoError(t, err)
	m.observe(samples)
	require.InDelta(t, 1, testutil.ToFloat64(m.outcomes.WithLabelValues("reverted", "")), 0)

	families, err := reg.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != "hololive_youtube_collection_time_finalization_outcomes" {
			continue
		}

		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				require.NotEqual(t, "exhausted", label.GetValue())
			}
		}
	}
}
