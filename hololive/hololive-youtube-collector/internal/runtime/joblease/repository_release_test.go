package joblease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestReleaseReasonsUseDistinctShapesAndPreserveFailureHistory(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		reason    ReleaseReason
		state     string
		errorCode string
	}{
		{name: "shutdown", reason: ReleaseShutdown, state: "DEFERRED", errorCode: string(contract.ErrorShutdownRelease)},
		{name: "renew", reason: ReleaseRenewFail, state: "DEFERRED", errorCode: string(contract.ErrorRenewFailedRelease)},
		{name: "superseded", reason: ReleaseSuperseded, state: "IDLE", errorCode: string(contract.ErrorSupersededRelease)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			pool := dbtest.NewPool(t)
			seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
			repository := newTestRepository(t, pool)
			spec := communityJob()
			first := mustAcquireLease(t, ctx, repository, spec, "collector-a")
			mustDeferLease(t, ctx, first, string(contract.ErrorCollectionFailed), string(contract.ClassTransient), "prior failure")
			prior := readFailureDiagnostics(t, ctx, pool, spec.JobKey)
			scheduledFor := first.Proof().ScheduledFor
			makeRetryDue(t, ctx, pool, spec.JobKey)
			second := mustAcquireLease(t, ctx, repository, spec, "collector-b")
			if err := second.Release(ctx, testCase.reason); err != nil {
				t.Fatalf("release %s: %v", testCase.name, err)
			}
			assertReleasedLeaseShape(t, ctx, pool, spec.JobKey, scheduledFor, testCase.state, testCase.errorCode)
			assertFailureDiagnostics(t, ctx, pool, spec.JobKey, prior.code, prior.class, prior.detail, prior.at)
			if testCase.reason == ReleaseSuperseded {
				if _, err := repository.Acquire(ctx, spec, "collector-c"); err != nil {
					t.Fatalf("acquire after superseded release: %v", err)
				}
			}
		})
	}
}

func TestOrdinaryDeferSQLInvalidTupleDoesNotUpdateActiveLease(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{"channel:a", contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	lease := mustAcquireLease(t, ctx, repository, communityJob(), "collector-a")
	proof := lease.Proof()
	var jobKey string
	err := pool.QueryRow(ctx, mustSQL("repository_lease_defer_0144_12.sql"),
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		time.Now().UTC().Add(time.Second), "not_a_code", "TRANSIENT", "detail",
		repository.config.MinRetryDelay.Milliseconds(), repository.config.MaxRetryDelay.Milliseconds(),
	).Scan(&jobKey)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("invalid ordinary defer tuple error = %v, want no rows", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT slot_state FROM youtube_collection_job_leases WHERE job_key = $1`, proof.JobKey).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "ACTIVE" {
		t.Fatalf("invalid tuple changed state to %s", state)
	}
}

func TestReleaseReasonRejectsUnknownCode(t *testing.T) {
	if (ReleaseReason("not_a_release")).Valid() {
		t.Fatal("unknown release reason must be invalid")
	}
	lease := &JobLease{}
	if err := lease.Release(context.Background(), ReleaseReason("not_a_release")); err == nil {
		t.Fatal("invalid release reason must fail")
	}
}

func assertReleasedLeaseShape(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	jobKey string,
	scheduledFor time.Time,
	wantState, wantError string,
) {
	t.Helper()
	var state, errorCode string
	var gotScheduled time.Time
	var retryAt *time.Time
	var nextDue time.Time
	var owner *string
	if err := pool.QueryRow(ctx, `
		SELECT slot_state, last_error_code, scheduled_for, retry_not_before, next_due_at, owner_instance
		FROM youtube_collection_job_leases
		WHERE job_key = $1
	`, jobKey).Scan(&state, &errorCode, &gotScheduled, &retryAt, &nextDue, &owner); err != nil {
		t.Fatal(err)
	}
	if state != wantState || errorCode != wantError || !gotScheduled.Equal(scheduledFor) || owner != nil {
		t.Fatalf("release shape = state:%s error:%s scheduled:%s owner:%v, want state:%s error:%s scheduled:%s owner:nil",
			state, errorCode, gotScheduled, owner, wantState, wantError, scheduledFor)
	}
	if wantState == "IDLE" {
		if retryAt != nil {
			t.Fatalf("superseded retry_not_before = %v, want NULL", retryAt)
		}
		if nextDue.After(time.Now().UTC().Add(time.Second)) {
			t.Fatalf("superseded next_due_at = %s, want <= now", nextDue)
		}
		return
	}
	if retryAt == nil {
		t.Fatal("deferred release must set retry_not_before")
	}
}
