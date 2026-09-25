package runtime

import (
	_ "embed"
	jsonv2 "encoding/json/v2"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	dbtest "github.com/kapu/hololive-dbtest"
)

//go:embed testqueries/collection_target_observability.sql
var collectionTargetFixture string

func TestCollectionTargetSnapshotIncludesUnqueuedAndExcludesInactive(t *testing.T) {
	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(t.Context(), collectionTargetFixture); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(t.Context(), mustSQL("collection_target_observability.sql"))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	samples, err := scanCollectionTargets(rows)
	if err != nil {
		t.Fatal(err)
	}

	if len(samples) != 4 {
		t.Fatalf("sample count = %d", len(samples))
	}

	assertExecutableCollectionSamples(t, samples)

	// 현재 viewer target이 없어도 양방향 상태 불일치와 예정 시각 초과를 집계합니다.
	live := samples[0]
	got := [...]int64{live.live, live.upcoming, live.other, live.stateMismatch, live.pastDue, live.pastDue7d}

	if got != [...]int64{2, 3, 2, 3, 2, 1} {
		t.Fatalf("operational live diagnostics = %+v", live)
	}

	m := newCollectionTargetMetrics(prometheus.NewPedanticRegistry())
	now := time.Now()
	m.observe(samples, now, nil)

	if testutil.ToFloat64(m.success) != 1 {
		t.Fatal("four executable jobs did not produce a successful snapshot")
	}

	assertCollectionSnapshotMetrics(t, m, now)

	m.observe(nil, now.Add(time.Minute), errors.New("database unavailable"))

	if testutil.ToFloat64(m.success) != 0 {
		t.Fatal("failed observation was reported as successful")
	}

	assertCollectionSnapshotMetrics(t, m, now)

	m.observe(samples[:3], now.Add(2*time.Minute), nil)

	if testutil.ToFloat64(m.success) != 0 {
		t.Fatal("incomplete observation was reported as successful")
	}

	assertCollectionSnapshotMetrics(t, m, now)
}

func assertCollectionSnapshotMetrics(t *testing.T, m *collectionTargetMetrics, now time.Time) {
	t.Helper()

	if testutil.ToFloat64(m.targets.WithLabelValues("youtubejs_channel_live")) != 4 ||
		testutil.ToFloat64(m.requiredRate.WithLabelValues("youtubejs_content")) != 2.0/120 ||
		testutil.ToFloat64(m.lastSuccess) != float64(now.Unix()) {
		t.Fatal("observation replaced target demand or last successful timestamp")
	}

	for state, want := range map[string]float64{"LIVE": 2, "UPCOMING": 3, "other": 2} {
		if got := testutil.ToFloat64(m.liveState.WithLabelValues(state)); got != want {
			t.Fatalf("live state %s = %v, want %v", state, got, want)
		}
	}

	for reason, want := range map[string]float64{"state_mismatch": 3, "scheduled_before_now": 2, "scheduled_overdue_7d": 1} {
		if got := testutil.ToFloat64(m.liveReview.WithLabelValues(reason)); got != want {
			t.Fatalf("live review %s = %v, want %v", reason, got, want)
		}
	}
}

func TestCollectionTargetSnapshotRejectsInvalidProjection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update string
	}{
		{name: "missing"},
		{name: "expired", update: "UPDATE youtube_collection_projection_generations SET valid_until = now() - interval '1 second' WHERE status = 'CURRENT'"},
		{name: "retired", update: "UPDATE youtube_collection_projection_generations SET status = 'RETIRED' WHERE status = 'CURRENT'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)

			if tc.update != "" {
				if _, err := pool.Exec(t.Context(), collectionTargetFixture); err != nil {
					t.Fatal(err)
				}

				if _, err := pool.Exec(t.Context(), tc.update); err != nil {
					t.Fatal(err)
				}
			}

			rows, err := pool.Query(t.Context(), mustSQL("collection_target_observability.sql"))
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()

			if _, err := scanCollectionTargets(rows); err == nil {
				t.Fatal("invalid projection was reported as a valid empty fleet")
			}
		})
	}
}

type collectionStatePlanNode struct {
	Relation string                    `json:"Relation Name"`
	Rows     float64                   `json:"Actual Rows"`
	Removed  float64                   `json:"Rows Removed by Filter"`
	Loops    float64                   `json:"Actual Loops"`
	Plans    []collectionStatePlanNode `json:"Plans"`
}

func TestCollectionLiveStateWorkExcludesRetainedEndedHistory(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, collectionTargetFixture); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_reconciliation_heads (video_id, status)
		SELECT 'retained-' || n, 'ENDED' FROM generate_series(1, 30000) n;
		INSERT INTO youtube_live_sessions (video_id, channel_id, status)
		SELECT 'retained-' || n, 'stale', 'ENDED' FROM generate_series(1, 30000) n;
		ANALYZE youtube_live_reconciliation_heads;
		ANALYZE youtube_live_sessions;
	`); err != nil {
		t.Fatal(err)
	}

	var raw []byte

	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+mustSQL("collection_target_observability.sql")).Scan(&raw); err != nil {
		t.Fatal(err)
	}

	var plans []struct {
		Plan collectionStatePlanNode `json:"Plan"`
	}

	if err := jsonv2.Unmarshal(raw, &plans); err != nil {
		t.Fatal(err)
	}

	if len(plans) != 1 {
		t.Fatalf("query plans = %d", len(plans))
	}

	if visits := collectionStateVisits(plans[0].Plan); visits > 256 {
		t.Fatalf("state snapshot visited %.0f live-state rows; retained history must not dominate the active population", visits)
	}
}

func collectionStateVisits(node collectionStatePlanNode) float64 {
	var visits float64

	if node.Relation == "youtube_live_reconciliation_heads" || node.Relation == "youtube_live_sessions" {
		visits = (node.Rows + node.Removed) * node.Loops
	}

	for _, child := range node.Plans {
		visits += collectionStateVisits(child)
	}

	return visits
}

func assertExecutableCollectionSamples(t *testing.T, samples []collectionTargetSample) {
	t.Helper()

	wantRates := map[string]float64{
		"community_collect":          1.0 / 120,
		"youtubejs_content":          2.0 / 120,
		"youtubejs_channel_live":     4.0 / 120,
		"youtubejs_channel_metadata": 1.0 / 120,
	}

	for i := range samples {
		s := &samples[i]
		rate, ok := wantRates[s.kind]

		if !ok || s.requiredRate != rate {
			t.Fatalf("unexpected executable job demand: %+v", s)
		}

		delete(wantRates, s.kind)

		if s.kind == "youtubejs_channel_live" {
			got := [...]int64{s.targets, s.stale, s.neverCompleted, s.due}
			if got != [...]int64{4, 2, 1, 2} || s.oldestCompletionAge < 600 || s.oldestDueAge < 600 {
				t.Fatalf("live collection snapshot = %+v", s)
			}
		} else if s.targets != 1 || s.neverCompleted != 1 || s.stale != 0 || s.due != 1 || s.oldestCompletionAge != 0 {
			t.Fatalf("uncompleted bundled collection snapshot = %+v", s)
		}
	}

	if len(wantRates) != 0 {
		t.Fatalf("missing executable jobs: %v", wantRates)
	}
}
