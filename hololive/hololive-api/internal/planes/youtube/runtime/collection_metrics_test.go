package runtime

import (
	_ "embed"
	jsonv2 "encoding/json/v2"
	"errors"
	"math"
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

	rows, err := pool.Query(t.Context(), mustSQL("collection_target_observability.sql"), defaultLiveFreshnessBudget().Milliseconds())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	samples, err := scanCollectionTargets(rows)
	if err != nil {
		t.Fatal(err)
	}

	if len(samples) < 3 {
		t.Fatalf("snapshot cannot exercise full and truncated observations: %+v", samples)
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
		t.Fatal("all executable jobs did not produce a successful snapshot")
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

// TestCollectionTargetSnapshotDefersSleepingTargets는 not_before가 미래인 영상 확인 target이 due·미완료·stale로
// 보이지 않고 수요를 신선도 예산당 1회로 세며, 지난 not_before는 즉시 due로 돌아오는지 확인한다.
func TestCollectionTargetSnapshotDefersSleepingTargets(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()

	if _, err := pool.Exec(ctx, collectionTargetFixture); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until,created_at,not_before)
		SELECT generation, subject, 'video_live_check', 20, 120000, true, now()+interval '1 hour', now()-interval '10 minutes', now()+wake
		FROM youtube_collection_projection_generations CROSS JOIN (VALUES
		 ('video-sleeping', interval '2 minutes'),
		 ('video-awake', interval '-1 minute'),
		 ('video-slept', interval '1 minute')
		) AS seed(subject, wake)
		WHERE status='CURRENT';
		-- 완료 뒤 slot 시각은 지났지만 더 새로운 신선도 근거로 잠든 job은 stale·due가 아니다.
		INSERT INTO youtube_collection_job_leases(job_key,provider,job_class,collection_job_kind,subject_key,
		projection_generation,poll_interval_ms,slot_state,scheduled_for,next_due_at,last_completed_at)
		SELECT 'collector:youtubejs:youtubejs_video_live:video-slept','youtubejs','SUBJECT','youtubejs_video_live','video-slept',
		generation,120000,'IDLE',now()-interval '12 minutes',now()-interval '5 minutes',now()-interval '10 minutes'
		FROM youtube_collection_projection_generations WHERE status='CURRENT';
	`); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(ctx, mustSQL("collection_target_observability.sql"), defaultLiveFreshnessBudget().Milliseconds())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	samples, err := scanCollectionTargets(rows)
	if err != nil {
		t.Fatal(err)
	}

	if len(samples) != collectionTargetJobCount {
		t.Fatalf("snapshot rows = %d, want %d", len(samples), collectionTargetJobCount)
	}

	for i := range samples {
		s := &samples[i]
		if s.kind != "youtubejs_video_live" {
			continue
		}

		// video-stale(fixture)과 video-awake만 확인 가능하고 미완료다. video-slept는 완료 나이가 커도 잠들어 있다.
		got := [...]int64{s.targets, s.neverCompleted, s.stale, s.due}
		if got != [...]int64{4, 2, 0, 2} || s.oldestDueAge < 60 || s.oldestCompletionAge != 0 {
			t.Fatalf("video live snapshot = %+v", s)
		}

		budget := float64(defaultLiveFreshnessBudget().Milliseconds())
		if want := 2*1000.0/120000 + 2*1000.0/budget; math.Abs(s.requiredRate-want) > 1e-9 {
			t.Fatalf("video live required rate = %v, want %v", s.requiredRate, want)
		}

		return
	}

	t.Fatal("video live snapshot row is missing")
}

func assertCollectionSnapshotMetrics(t *testing.T, m *collectionTargetMetrics, now time.Time) {
	t.Helper()

	if testutil.ToFloat64(m.targets.WithLabelValues("youtubejs_channel_live")) != 4 ||
		testutil.ToFloat64(m.requiredRate.WithLabelValues("youtubejs_content")) != 2.0/120 ||
		testutil.ToFloat64(m.requiredRate.WithLabelValues("youtubejs_channel_live")) != 4.0/120 ||
		testutil.ToFloat64(m.requiredRate.WithLabelValues("youtubejs_channel_live_check")) != 2.0/120 ||
		testutil.ToFloat64(m.requiredRate.WithLabelValues("youtubejs_video_live")) != 1.0/120 ||
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

			rows, err := pool.Query(t.Context(), mustSQL("collection_target_observability.sql"), defaultLiveFreshnessBudget().Milliseconds())
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

	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+mustSQL("collection_target_observability.sql"), defaultLiveFreshnessBudget().Milliseconds()).Scan(&raw); err != nil {
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

	// 방송 탭 snapshot과 채널 /live 확인은 채널마다 각자 job으로 RPC 1회를 보낸다.
	// disabled 채널 확인 target은 수요에 들어가지 않는다.
	wantRates := map[string]float64{
		"community_collect":            1.0 / 120,
		"youtubejs_content":            2.0 / 120,
		"youtubejs_channel_live":       4.0 / 120,
		"youtubejs_channel_live_check": 2.0 / 120,
		"youtubejs_channel_metadata":   1.0 / 120,
		"youtubejs_video_live":         1.0 / 120,
	}

	for i := range samples {
		s := &samples[i]
		rate, ok := wantRates[s.kind]

		if !ok || s.requiredRate != rate {
			t.Fatalf("unexpected executable job demand: %+v", s)
		}

		delete(wantRates, s.kind)

		assertCollectionJobProgress(t, s)
	}

	if len(wantRates) != 0 {
		t.Fatalf("missing executable jobs: %v", wantRates)
	}
}

func assertCollectionJobProgress(t *testing.T, s *collectionTargetSample) {
	t.Helper()

	switch s.kind {
	case "youtubejs_channel_live":
		got := [...]int64{s.targets, s.stale, s.neverCompleted, s.due}
		if got != [...]int64{4, 2, 1, 2} || s.oldestCompletionAge < 600 || s.oldestDueAge < 600 {
			t.Fatalf("live collection snapshot = %+v", s)
		}
	case "youtubejs_channel_live_check":
		// 같은 채널의 오래된 방송 탭 완료가 확인 job의 최근 완료·다음 슬롯을 가리지 않는다.
		got := [...]int64{s.targets, s.stale, s.neverCompleted, s.due}
		if got != [...]int64{2, 0, 1, 1} || s.oldestCompletionAge < 30 || s.oldestCompletionAge >= 600 || s.oldestDueAge < 600 {
			t.Fatalf("channel live check collection snapshot = %+v", s)
		}
	default:
		if s.targets != 1 || s.neverCompleted != 1 || s.stale != 0 || s.due != 1 || s.oldestCompletionAge != 0 {
			t.Fatalf("uncompleted bundled collection snapshot = %+v", s)
		}
	}
}
