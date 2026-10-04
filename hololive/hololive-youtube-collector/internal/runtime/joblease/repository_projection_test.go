package joblease

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func TestAcquireBundleRechecksHeaderExpiryAfterGuard(t *testing.T) {
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(t.Context())); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback projection expiry transaction: %v", rollbackErr)
		}
	}()

	if _, current, err := lockProjectionGuard(t.Context(), tx); err != nil || !current {
		t.Fatalf("guard: %v %v", current, err)
	}

	if _, err := tx.Exec(t.Context(), mustTestSQL("expire_projection.sql"), generation); err != nil {
		t.Fatal(err)
	}

	spec := communityJob()
	scope := collection.MembershipScope{ExactSubject: true, Kinds: []string{string(contract.KindCommunityPage)}}

	if _, err := verifyAcquireTargets(t.Context(), tx, spec, []contract.ObservationKind{contract.KindCommunityPage}, scope, generation); !errors.Is(err, collection.ErrProjectionStale) {
		t.Fatalf("bundle accepted expired header: %v", err)
	}
}

func seedCandidateScale(t *testing.T, pool *pgxpool.Pool, count int) int64 {
	t.Helper()

	generation := seedProjection(t, pool, nil)
	if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_collection_targets (projection_generation, subject_key, observation_kind, priority, poll_interval_ms, enabled, member_since_generation)
		SELECT $1, 'channel:' || lpad(i::text, 5, '0'), 'community_page', 50, 60000, TRUE, $1 FROM generate_series(1,$2::int) AS i`, generation, count); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), `UPDATE youtube_collection_projection_generations SET row_count=$2 WHERE generation=$1`, generation, count); err != nil {
		t.Fatal(err)
	}

	return generation
}

func TestCandidateLateKeyBeyondLeaseBatchAndLimit(t *testing.T) {
	pool := dbtest.NewPool(t)
	generation := seedCandidateScale(t, pool, 513)

	if _, err := pool.Exec(t.Context(), `INSERT INTO youtube_collection_job_leases (job_key, provider, job_class, collection_job_kind, subject_key, projection_generation, poll_interval_ms, scheduled_for, next_due_at)
		SELECT 'collector:youtubejs:community_collect:' || subject_key, 'youtubejs', 'SUBJECT', 'community_collect', subject_key, $1, 60000, clock_timestamp(), clock_timestamp()+INTERVAL '1 hour'
		FROM youtube_collection_targets WHERE projection_generation=$1 AND subject_key <> 'channel:00513'`, generation); err != nil {
		t.Fatal(err)
	}

	repository := newTestRepository(t, pool)
	page := candidatePage(t, repository, contract.ProviderYouTubeJS, "community_collect", nil, 1)

	if len(page.Jobs) != 1 || page.Jobs[0].SubjectKey != "channel:00513" || page.Truncated {
		t.Fatalf("late due key hidden: %+v", page)
	}
}

func TestFourCollectorsCompeteAndRestartFromDurableLease(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})

	repositories := make([]*Repository, 4)
	pages := make([]CandidatePage, 4)

	for i := range repositories {
		collectorPool, _ := measuredCandidatePool(t, pool)

		repositories[i] = newTestRepository(t, collectorPool)
		pages[i] = candidatePage(t, repositories[i], contract.ProviderYouTubeJS, "community_collect", nil, 1)

		if len(pages[i].Jobs) != 1 {
			t.Fatalf("collector %d missing shared due job", i)
		}
	}

	var wg sync.WaitGroup

	leases := make([]*JobLease, 4)
	errs := make([]error, 4)
	start := make(chan struct{})

	for i := range repositories {
		repositories[i].config.LeaseTTL = time.Minute

		wg.Go(func() {
			<-start

			leases[i], errs[i] = repositories[i].Acquire(t.Context(), &pages[i].Jobs[0], fmt.Sprintf("collector-%d", i))
		})
	}

	close(start)
	wg.Wait()

	winners := 0

	for i, lease := range leases {
		if lease != nil {
			winners++
			continue
		}

		if !errors.Is(errs[i], ErrNotAcquired) {
			t.Fatalf("collector %d: %v", i, errs[i])
		}
	}

	if winners != 1 {
		t.Fatalf("holders=%d", winners)
	}

	restarted := newTestRepository(t, pool)
	if page := candidatePage(t, restarted, contract.ProviderYouTubeJS, "community_collect", nil, 1); len(page.Jobs) != 0 {
		t.Fatalf("restart ignored durable ACTIVE lease: %+v", page)
	}
}

func TestCandidatesObserveCurrentEligibilityAndHeaderExpiry(t *testing.T) {
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	job := mustTestJob(t, contract.ProviderYouTubeJS, "community_collect")

	if _, err := repository.CurrentProjectionGeneration(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), mustTestSQL("set_not_before.sql"), generation, time.Hour.Milliseconds()); err != nil {
		t.Fatal(err)
	}

	page, err := repository.CandidatesForProjection(t.Context(), generation, job, nil, 1)
	if err != nil || len(page.Jobs) != 0 {
		t.Fatalf("현재 eligibility: page=%v err=%v", page, err)
	}

	if _, clearErr := pool.Exec(t.Context(), `UPDATE youtube_collection_targets SET not_before=NULL WHERE projection_generation=$1`, generation); clearErr != nil {
		t.Fatal(clearErr)
	}

	page, err = repository.CandidatesForProjection(t.Context(), generation, job, nil, 1)
	if err != nil || len(page.Jobs) != 1 {
		t.Fatalf("NULL eligibility 복귀: page=%v err=%v", page, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := repository.CurrentProjectionGeneration(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("취소된 header 조회: %v", err)
	}

	if _, err := pool.Exec(t.Context(), mustTestSQL("expire_projection.sql"), generation); err != nil {
		t.Fatal(err)
	}

	if _, err := repository.CandidatesForProjection(t.Context(), generation, job, nil, 1); !errors.Is(err, collection.ErrProjectionStale) {
		t.Fatalf("만료된 header 입장: %v", err)
	}
}

func TestCandidatesIncludeLastMaximumLegalSubject(t *testing.T) {
	pool := dbtest.NewPool(t)
	generation := seedCandidateScale(t, pool, contract.MaxProjectionTargetCount)

	if _, err := pool.Exec(t.Context(), `UPDATE youtube_collection_targets SET
        subject_key=rpad(subject_key,256,'x'),
        not_before=CASE WHEN subject_key='channel:10000' THEN NULL ELSE statement_timestamp()+INTERVAL '1 hour' END
        WHERE projection_generation=$1`, generation); err != nil {
		t.Fatal(err)
	}

	repository := newTestRepository(t, pool)
	page := candidatePage(t, repository, contract.ProviderYouTubeJS, "community_collect", nil, 1)

	if len(page.Jobs) != 1 || len(page.Jobs[0].SubjectKey) != 256 || page.Jobs[0].SubjectKey[:13] != "channel:10000" || page.Truncated {
		t.Fatalf("최대 대상 집합의 마지막 후보를 놓쳤습니다: %+v", page)
	}
}
