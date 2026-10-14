package joblease

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// 예열 후 후보 2회와 일반 획득 4회(BEGIN/guard/선점/종료)만 허용한다. 최초 생성 승자만 시각 확정 1회가 추가된다.
// 네 collector가 같은 후보를 읽어도 최초 생성·만료 인계·IDLE·DEFERRED마다 소유자는 하나여야 한다.
func TestFourCollectorsCandidateAcquireTraffic(t *testing.T) {
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		for _, exact := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/exact=%t", mode, exact), func(t *testing.T) {
				checkAcquireTrafficScenario(t, mode, exact)
			})
		}
	}
}

func checkAcquireTrafficScenario(t *testing.T, mode string, exact bool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	spec := communityJob()
	targets := []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}}

	if !exact {
		spec = holodexLiveJob()
		targets = []leaseTarget{
			{subjectUCA, contract.KindLiveSnapshot, time.Minute, true},
			{subjectUCB, contract.KindLiveSnapshot, time.Minute, true},
		}
	}

	generation := seedProjection(t, pool, targets)
	repositories, traffic := warmAcquireTraffic(t, pool, spec, mode)

	var previous contract.LeaseProof

	for i, state := range []string{"missing", "expired", "idle", "deferred"} {
		prepareAcquireTrafficState(t, pool, spec.JobKey, state)

		proof := competeAcquireTraffic(t, repositories, traffic, spec, state == "missing")
		if proof.FenceEpoch != int64(i+1) || proof.ProjectionGeneration != generation {
			t.Fatalf("%s proof = %+v", state, proof)
		}

		if (state == "expired" || state == "deferred") && !proof.ScheduledFor.Equal(previous.ScheduledFor) {
			t.Fatalf("%s changed slot: previous=%s acquired=%s", state, previous.ScheduledFor, proof.ScheduledFor)
		}

		if state == "idle" && !proof.ScheduledFor.After(previous.ScheduledFor) {
			t.Fatalf("IDLE slot did not advance: previous=%s acquired=%s", previous.ScheduledFor, proof.ScheduledFor)
		}

		var (
			count       int
			storedExact bool
		)

		if err := pool.QueryRow(t.Context(), `SELECT membership_target_count, membership_exact_subject
			FROM youtube_collection_job_leases WHERE job_key=$1`, spec.JobKey).Scan(&count, &storedExact); err != nil {
			t.Fatal(err)
		}

		if count != len(targets) || storedExact != exact {
			t.Fatalf("%s membership count=%d exact=%t", state, count, storedExact)
		}

		previous = proof

		if state == "missing" {
			// BEFORE INSERT는 ON CONFLICT보다 먼저 실행된다. 기존 행 경로의 삽입 시도 자체를 검출한다.
			if _, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_lease_insert() RETURNS trigger
				LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'existing lease attempted INSERT'; END; $$;
				CREATE TRIGGER reject_lease_insert BEFORE INSERT ON youtube_collection_job_leases
				FOR EACH ROW EXECUTE FUNCTION reject_lease_insert()`); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func warmAcquireTraffic(t *testing.T, pool *pgxpool.Pool, spec *JobSpec, mode string) ([4]*Repository, [4]*candidateTraffic) {
	t.Helper()

	var (
		repositories [4]*Repository
		traffic      [4]*candidateTraffic
	)

	for i := range repositories {
		measured, counters := measuredCandidatePool(t, pool)
		if _, err := measured.Exec(t.Context(), "SET plan_cache_mode = "+mode); err != nil {
			t.Fatal(err)
		}

		repositories[i], traffic[i] = newTestRepository(t, measured), counters
		repositories[i].config.LeaseTTL = time.Minute

		prepareAcquireTrafficState(t, pool, spec.JobKey, "missing")
		mustAcquireLease(t, repositories[i], spec, "warm-owner")
		candidatePage(t, repositories[i], spec.Provider, spec.CollectionJobKind, nil, 1)

		if _, err := repositories[i].Acquire(t.Context(), spec, "warm-contender"); !errors.Is(err, ErrNotAcquired) {
			t.Fatalf("warm active lease: %v", err)
		}
	}

	return repositories, traffic
}

func prepareAcquireTrafficState(t *testing.T, pool *pgxpool.Pool, jobKey, state string) {
	t.Helper()

	var query string

	switch state {
	case "missing":
		query = `DELETE FROM youtube_collection_job_leases WHERE job_key=$1`
	case "expired":
		query = mustTestSQL("expire_lease.sql")
	case "idle":
		query = `UPDATE youtube_collection_job_leases SET slot_state='IDLE', owner_instance=NULL,
			lease_expires_at=NULL, next_due_at=clock_timestamp() WHERE job_key=$1`
	case "deferred":
		query = `UPDATE youtube_collection_job_leases SET slot_state='DEFERRED', owner_instance=NULL,
			lease_expires_at=NULL, retry_not_before=clock_timestamp()-INTERVAL '1 second' WHERE job_key=$1`
	default:
		t.Fatalf("unknown acquire traffic state %q", state)
	}

	if _, err := pool.Exec(t.Context(), query, jobKey); err != nil {
		t.Fatal(err)
	}
}

func competeAcquireTraffic(t *testing.T, repositories [4]*Repository, traffic [4]*candidateTraffic, spec *JobSpec, missing bool) contract.LeaseProof {
	t.Helper()

	var pages [4]CandidatePage

	for i, repository := range repositories {
		traffic[i].reset()

		pages[i] = candidatePage(t, repository, spec.Provider, spec.CollectionJobKind, nil, 1)
		if len(pages[i].Jobs) != 1 || pages[i].Jobs[0].JobKey != spec.JobKey {
			t.Fatalf("collector %d candidate = %+v", i, pages[i])
		}
	}

	var (
		wg     sync.WaitGroup
		leases [4]*JobLease
		errs   [4]error
	)

	start := make(chan struct{})

	for i, repository := range repositories {
		wg.Go(func() {
			<-start

			leases[i], errs[i] = repository.Acquire(t.Context(), &pages[i].Jobs[0], fmt.Sprintf("collector-%d", i))
		})
	}

	close(start)
	wg.Wait()

	var winner contract.LeaseProof

	winners := 0

	for i, lease := range leases {
		if lease != nil {
			winner = lease.Proof()
			winners++
		} else if !errors.Is(errs[i], ErrNotAcquired) {
			t.Fatalf("collector %d acquire: %v", i, errs[i])
		}

		assertAcquireTraffic(t, i, traffic[i], missing && lease != nil)
	}

	if winners != 1 {
		t.Fatalf("holders=%d, want 1", winners)
	}

	return winner
}

func assertAcquireTraffic(t *testing.T, collector int, measured *candidateTraffic, created bool) {
	t.Helper()

	wantStatements := int64(6)

	if created {
		wantStatements++
	}

	if measured.statements.Load() != wantStatements || measured.headers.Load() != 1 || measured.candidates.Load() != 1 {
		t.Fatalf("collector %d statements=%d headers=%d candidates=%d, want %d/1/1", collector,
			measured.statements.Load(), measured.headers.Load(), measured.candidates.Load(), wantStatements)
	}

	if measured.writes.Load() > wantStatements || measured.rx.Load() == 0 || measured.tx.Load() == 0 {
		t.Fatalf("collector %d wire writes=%d rx=%d tx=%d", collector, measured.writes.Load(), measured.rx.Load(), measured.tx.Load())
	}

	t.Logf("collector=%d candidate_acquire_wire_writes=%d rx=%d tx=%d", collector, measured.writes.Load(), measured.rx.Load(), measured.tx.Load())
}
