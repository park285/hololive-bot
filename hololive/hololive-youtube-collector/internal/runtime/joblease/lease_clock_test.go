package joblease

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// 한 전이 문장이 기록하는 시각 열은 DB 시각 하나에서 나온다. 열마다 clock_timestamp()를 부르면
// lease_expires_at과 updated_at이 TTL보다 마이크로초 단위로 어긋난다.
func TestLeaseTransitionsStampOneDBInstant(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	ctx := t.Context()
	seedProjection(t, pool, []leaseTarget{{"clock-subject", contract.KindVideoLiveCheck, time.Minute, true}})

	repository := newTestRepository(t, pool)
	ttl := repository.config.LeaseTTL

	jobs := candidateJobs(t, repository, contract.ProviderYouTubeJS, "youtubejs_video_live", 1)
	if len(jobs) != 1 {
		t.Fatalf("candidate jobs = %+v, want one", jobs)
	}

	created := mustAcquireLease(t, repository, &jobs[0], "collector-a")
	jobKey := created.Proof().JobKey
	assertLeaseTTLFromUpdatedAt(t, pool, jobKey, ttl, "created acquire")

	if err := created.Renew(ctx); err != nil {
		t.Fatal(err)
	}

	assertLeaseTTLFromUpdatedAt(t, pool, jobKey, ttl, "renew")

	if err := created.Complete(ctx); err != nil {
		t.Fatal(err)
	}

	var completedAtUpdate bool

	if err := pool.QueryRow(ctx, `SELECT last_completed_at = updated_at FROM youtube_collection_job_leases WHERE job_key = $1`, jobKey).
		Scan(&completedAtUpdate); err != nil {
		t.Fatal(err)
	}

	if !completedAtUpdate {
		t.Fatal("complete stamped last_completed_at and updated_at from different instants")
	}

	if _, err := pool.Exec(ctx, `UPDATE youtube_collection_job_leases SET next_due_at = now() - interval '1 minute' WHERE job_key = $1`, jobKey); err != nil {
		t.Fatal(err)
	}

	jobs = candidateJobs(t, repository, contract.ProviderYouTubeJS, "youtubejs_video_live", 1)
	if len(jobs) != 1 {
		t.Fatalf("due candidate jobs = %+v, want one", jobs)
	}

	mustAcquireLease(t, repository, &jobs[0], "collector-b")
	assertLeaseTTLFromUpdatedAt(t, pool, jobKey, ttl, "existing acquire")
}

func assertLeaseTTLFromUpdatedAt(t *testing.T, pool *pgxpool.Pool, jobKey string, ttl time.Duration, step string) {
	t.Helper()

	var (
		expiresAfterUpdate time.Duration
		slotNotAfterUpdate bool
	)

	if err := pool.QueryRow(t.Context(), `
SELECT lease_expires_at - updated_at, scheduled_for <= updated_at
FROM youtube_collection_job_leases
WHERE job_key = $1`, jobKey).Scan(&expiresAfterUpdate, &slotNotAfterUpdate); err != nil {
		t.Fatal(err)
	}

	if expiresAfterUpdate != ttl || !slotNotAfterUpdate {
		t.Fatalf("%s: lease_expires_at-updated_at = %s, scheduled_for<=updated_at = %t; want %s, true",
			step, expiresAfterUpdate, slotNotAfterUpdate, ttl)
	}
}
