package runtime

import (
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	dbtest "github.com/kapu/hololive-dbtest"
)

type collectionCadenceCase struct {
	name            string
	previous        time.Duration
	current         time.Duration
	state           string
	stateDue        time.Duration
	eligibility     time.Duration
	expired         bool
	wantDue         int64
	wantEffectiveAt time.Duration
}

func TestCollectionTargetCadenceMatchesSchedulerCandidates(t *testing.T) {
	const idleLeaseState = "IDLE"

	// collector 내부 패키지를 복제하지 않고 실제 후보 SQL을 같은 migration DB에서 실행한다.
	candidateSQL, err := os.ReadFile("../../../../../hololive-youtube-collector/internal/runtime/joblease/queries/repository_candidates_0144_02.sql")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []collectionCadenceCase{
		{name: "shortening_10m_to_1m", previous: 10 * time.Minute, current: time.Minute, state: idleLeaseState, wantDue: 1, wantEffectiveAt: -time.Minute},
		{name: "extension_keeps_due_slot", previous: time.Minute, current: 10 * time.Minute, state: idleLeaseState, wantDue: 1, wantEffectiveAt: -time.Minute},
		{name: "extension_keeps_future_slot", previous: 3 * time.Minute, current: 10 * time.Minute, state: idleLeaseState, wantEffectiveAt: time.Minute},
		{name: "active_unexpired", previous: 10 * time.Minute, current: time.Minute, state: "ACTIVE", stateDue: time.Minute, wantEffectiveAt: time.Minute},
		{name: "active_expired", previous: 10 * time.Minute, current: time.Minute, state: "ACTIVE", stateDue: -30 * time.Second, wantDue: 1, wantEffectiveAt: -30 * time.Second},
		{name: "deferred_future", previous: 10 * time.Minute, current: time.Minute, state: "DEFERRED", stateDue: time.Minute, wantEffectiveAt: time.Minute},
		{name: "deferred_ready", previous: 10 * time.Minute, current: time.Minute, state: "DEFERRED", stateDue: -30 * time.Second, wantDue: 1, wantEffectiveAt: -30 * time.Second},
		{name: "future_eligible_at", previous: 10 * time.Minute, current: time.Minute, state: idleLeaseState, eligibility: 3 * time.Minute, wantEffectiveAt: 3 * time.Minute},
		{name: "recently_eligible", previous: 10 * time.Minute, current: time.Minute, state: idleLeaseState, eligibility: -15 * time.Second, wantDue: 1, wantEffectiveAt: -15 * time.Second},
		{name: "expired_projection", previous: 10 * time.Minute, current: time.Minute, state: idleLeaseState, expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			generation, fixtureTime := seedCollectionCadence(t, pool, tc)
			assertCollectionCadenceCandidates(t, pool, string(candidateSQL), generation, tc)

			assertCollectionCadenceMetrics(t, pool, fixtureTime, tc)
		})
	}
}

func assertCollectionCadenceMetrics(t *testing.T, pool *pgxpool.Pool, fixtureTime time.Time, tc collectionCadenceCase) {
	t.Helper()

	var before, after time.Time

	if err := pool.QueryRow(t.Context(), "SELECT statement_timestamp()").Scan(&before); err != nil {
		t.Fatal(err)
	}

	rows, err := pool.Query(t.Context(), mustSQL("collection_target_observability.sql"), defaultLiveFreshnessBudget().Milliseconds())
	if err != nil {
		t.Fatal(err)
	}

	samples, scanErr := scanCollectionTargets(rows)
	rows.Close()

	metrics := newCollectionTargetMetrics(prometheus.NewPedanticRegistry())
	metrics.observe(samples, fixtureTime, scanErr)

	if tc.expired {
		if scanErr == nil || testutil.ToFloat64(metrics.success) != 0 {
			t.Fatalf("expired projection snapshot = %+v, err = %v; must not report a valid empty fleet", samples, scanErr)
		}

		return
	}

	if scanErr != nil {
		t.Fatal(scanErr)
	}

	if err := pool.QueryRow(t.Context(), "SELECT statement_timestamp()").Scan(&after); err != nil {
		t.Fatal(err)
	}

	if got := testutil.ToFloat64(metrics.success); got != 1 {
		t.Fatalf("snapshot success = %v, want 1", got)
	}

	const kind = "youtubejs_video_live"

	if got := testutil.ToFloat64(metrics.targets.WithLabelValues(kind)); got != 1 {
		t.Fatalf("current targets = %v, want 1", got)
	}

	if got := testutil.ToFloat64(metrics.due.WithLabelValues(kind)); got != float64(tc.wantDue) {
		t.Fatalf("metric due = %v, want scheduler candidate count %d", got, tc.wantDue)
	}

	// DB 시각으로 조회 구간을 감싸서 실제 effective due의 나이를 검증한다.
	// recently_eligible은 lease due가 더 오래됐어도 eligibility 이후의 나이만 허용한다.
	dueAt := fixtureTime.Add(tc.wantEffectiveAt)
	minimum := max(0, before.Sub(dueAt).Seconds())
	maximum := max(0, after.Sub(dueAt).Seconds())

	if got := testutil.ToFloat64(metrics.oldestDueAge.WithLabelValues(kind)); got < minimum || got > maximum {
		t.Fatalf("oldest due age = %v, want [%v, %v] for effective due %v", got, minimum, maximum, dueAt)
	}
}

func seedCollectionCadence(t *testing.T, pool *pgxpool.Pool, tc collectionCadenceCase) (int64, time.Time) {
	t.Helper()

	ctx := t.Context()

	var (
		generation int64
		now        time.Time
	)

	if err := pool.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
		VALUES ('CURRENT',1,repeat('c',64),statement_timestamp()+interval '1 hour',statement_timestamp())
		RETURNING generation,statement_timestamp()
	`).Scan(&generation, &now); err != nil {
		t.Fatal(err)
	}

	var eligibleAt *time.Time

	if tc.eligibility != 0 {
		eligibleAt = new(now.Add(tc.eligibility))
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,created_at,not_before)
		VALUES ($1,'cadence-video','video_live_check',20,$2,true,$3,$4)
	`, generation, tc.previous.Milliseconds(), now.Add(-10*time.Minute), eligibleAt); err != nil {
		t.Fatal(err)
	}

	var (
		owner                          *string
		leaseExpiresAt, retryNotBefore *time.Time
	)

	switch tc.state {
	case "ACTIVE":
		owner = new("cadence-collector")
		leaseExpiresAt = new(now.Add(tc.stateDue))
	case "DEFERRED":
		retryNotBefore = new(now.Add(tc.stateDue))
	}

	slot := now.Add(-2 * time.Minute)
	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_collection_job_leases(job_key,provider,job_class,collection_job_kind,subject_key,
		projection_generation,poll_interval_ms,slot_state,scheduled_for,next_due_at,last_completed_at,owner_instance,lease_expires_at,retry_not_before)
		VALUES ('collector:youtubejs:youtubejs_video_live:cadence-video','youtubejs','SUBJECT','youtubejs_video_live','cadence-video',
		$1,$2,$3,$4,$5,$4,$6,$7,$8)
	`, generation, tc.previous.Milliseconds(), tc.state, slot, slot.Add(tc.previous), owner, leaseExpiresAt, retryNotBefore); err != nil {
		t.Fatal(err)
	}

	// target 갱신 trigger가 runtime bundle의 현재 cadence를 갱신하되 완료한 lease의 이전 cadence는 보존한다.
	if _, err := pool.Exec(ctx, `
		UPDATE youtube_collection_targets SET poll_interval_ms=$2 WHERE projection_generation=$1
	`, generation, tc.current.Milliseconds()); err != nil {
		t.Fatal(err)
	}

	if tc.expired {
		if _, err := pool.Exec(ctx, `
			UPDATE youtube_collection_projection_generations SET valid_until=$2 WHERE generation=$1
		`, generation, now.Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
	}

	return generation, now
}

func assertCollectionCadenceCandidates(t *testing.T, pool *pgxpool.Pool, query string, generation int64, tc collectionCadenceCase) {
	t.Helper()

	rows, err := pool.Query(t.Context(), query, generation, []string{"video_live_check"}, "youtubejs", "youtubejs_video_live", []string{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var candidates, resultRows int64

	for rows.Next() {
		var (
			valid            bool
			subject          *string
			minimum, maximum *int64
		)

		if err := rows.Scan(&valid, &subject, &minimum, &maximum); err != nil {
			t.Fatal(err)
		}

		resultRows++

		if valid == tc.expired {
			t.Fatalf("scheduler projection valid = %v, expired = %v", valid, tc.expired)
		}

		if subject != nil {
			candidates++

			if *subject != "cadence-video" || minimum == nil || maximum == nil || *minimum != tc.current.Milliseconds() || *maximum != tc.current.Milliseconds() {
				t.Fatalf("unexpected scheduler candidate: subject = %v, intervals = %v/%v", subject, minimum, maximum)
			}
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if resultRows != 1 || candidates != tc.wantDue {
		t.Fatalf("scheduler result rows = %d, candidates = %d, want 1 and %d", resultRows, candidates, tc.wantDue)
	}
}
