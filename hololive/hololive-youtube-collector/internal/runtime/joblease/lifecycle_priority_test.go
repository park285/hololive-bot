package joblease

import (
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestVideoCheckExistingLivePrecedesNewUpcomingReview(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	seedProjection(t, pool, []leaseTarget{
		{"z-live", contract.KindVideoLiveCheck, time.Minute, true},
		{"a-upcoming", contract.KindVideoLiveCheck, time.Minute, true},
	})

	if _, err := pool.Exec(ctx, `UPDATE youtube_collection_targets SET priority=CASE WHEN subject_key='z-live' THEN 20 ELSE 19 END`); err != nil {
		t.Fatal(err)
	}

	repository := newTestRepository(t, pool)
	jobs := candidateJobs(t, repository, contract.ProviderYouTubeJS, "youtubejs_video_live", 2)

	if len(jobs) != 2 || jobs[0].SubjectKey != "z-live" {
		t.Fatalf("LIVE priority lost before first check: %+v", jobs)
	}

	lease, err := repository.Acquire(ctx, &jobs[0], "collector-a")
	if err != nil {
		t.Fatal(err)
	}

	if err := lease.Complete(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx, `UPDATE youtube_collection_job_leases SET next_due_at=now()-interval '1 minute' WHERE subject_key='z-live'`); err != nil {
		t.Fatal(err)
	}

	jobs = candidateJobs(t, repository, contract.ProviderYouTubeJS, "youtubejs_video_live", 1)
	if len(jobs) != 1 || jobs[0].SubjectKey != "z-live" {
		t.Fatalf("new UPCOMING discovery delayed existing LIVE: %+v", jobs)
	}
}
