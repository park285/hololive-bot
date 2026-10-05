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
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func TestDeferRetryDeadlineUsesDatabaseClockAndPreservesNotBefore(t *testing.T) {
	for _, test := range []struct {
		name      string
		notBefore bool
		past      bool
	}{
		{name: "ordinary future stays bounded"},
		{name: "explicit future exceeds maximum", notBefore: true},
		{name: "explicit past uses database minimum", notBefore: true, past: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})

			repository := newTestRepository(t, pool)
			lease := mustAcquireLease(t, repository, communityJob(), "collector-a")
			proof := lease.Proof()
			before := retryDatabaseClock(t, pool)
			at := before.Add(10 * time.Minute)

			if test.past {
				at = before.Add(-time.Hour)
			}

			input := retryDeadlineInput(t, at, test.notBefore)
			if err := lease.Defer(t.Context(), input); err != nil {
				t.Fatal(err)
			}

			after := retryDatabaseClock(t, pool)

			stored := readRetryDeadlineWithContract(t.Context(), t, pool, proof)
			assertStoredRetryDeadline(t, input, stored, before, after)

			if test.notBefore && !test.past {
				if _, err := repository.Acquire(t.Context(), communityJob(), "collector-b"); !errors.Is(err, ErrNotAcquired) {
					t.Fatalf("reacquired before provider deadline: %v", err)
				}

				if err := lease.Defer(t.Context(), input); !errors.Is(err, collection.ErrFenceLost) {
					t.Fatalf("released fence could defer again: %v", err)
				}
			}
		})
	}
}

func readRetryDeadlineWithContract(ctx context.Context, t *testing.T, pool *pgxpool.Pool, proof contract.LeaseProof) time.Time {
	t.Helper()

	var (
		stored, scheduled  time.Time
		state, code, class string
		epoch              int64
		released           bool
	)

	if err := pool.QueryRow(ctx, `
		SELECT retry_not_before, scheduled_for, slot_state, fence_epoch,
		       last_failure_code, last_failure_class, owner_instance IS NULL AND lease_expires_at IS NULL
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, proof.JobKey).Scan(&stored, &scheduled, &state, &epoch, &code, &class, &released); err != nil {
		t.Fatal(err)
	}

	if state != "DEFERRED" || !released || !scheduled.Equal(proof.ScheduledFor) || epoch != proof.FenceEpoch ||
		code != string(contract.ErrorCooldown) || class != string(contract.ClassCooldown) {
		t.Fatalf("defer lost slot/fence/diagnostic: %s %s %d %s/%s released=%t", state, scheduled, epoch, code, class, released)
	}

	return stored
}

func assertStoredRetryDeadline(t *testing.T, input collection.DeferCollectionInput, stored, before, after time.Time) {
	t.Helper()

	if input.Schedule().IsNotBefore() && input.Schedule().At().After(after.Add(input.Bounds().Minimum)) {
		if !stored.Equal(input.Schedule().At()) {
			t.Fatalf("stored deadline = %s, want %s", stored, input.Schedule().At())
		}

		return
	}

	delay := input.Bounds().Maximum

	if input.Schedule().At().Before(before) {
		delay = input.Bounds().Minimum
	}

	if stored.Before(before.Add(delay)) || stored.After(after.Add(delay)) {
		t.Fatalf("stored deadline = %s, want database now + %s", stored, delay)
	}
}

func retryDeadlineInput(t *testing.T, at time.Time, notBefore bool) collection.DeferCollectionInput {
	t.Helper()

	schedule, err := collection.NewRetryAtSchedule(at)

	if notBefore {
		schedule, err = collection.NewRetryNotBeforeSchedule(at)
	}

	if err != nil {
		t.Fatal(err)
	}

	diagnostic, err := contract.NewFailureDiagnostic(contract.ErrorCooldown, contract.ClassCooldown, "provider cooldown")
	if err != nil {
		t.Fatal(err)
	}

	input, err := collection.NewDeferCollectionInput(diagnostic, testRetryBounds, schedule)
	if err != nil {
		t.Fatal(err)
	}

	return input
}

func retryDatabaseClock(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()

	var now time.Time

	if err := pool.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}

	return now
}

func TestDeferNotBeforeRejectsStaleFenceAndPreCanceledWrite(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})

	repository := newTestRepository(t, pool)
	lease := mustAcquireLease(t, repository, communityJob(), "collector-a")
	input := retryDeadlineInput(t, retryDatabaseClock(t, pool).Add(10*time.Minute), true)
	stale := *lease
	stale.proof.FenceEpoch++

	if err := stale.Defer(t.Context(), input); !errors.Is(err, collection.ErrFenceLost) {
		t.Fatalf("wrong fence defer = %v", err)
	}

	// 쿼리 전 취소는 전송 자체를 거절합니다. 전송 후 ctx 오류는 autocommit UPDATE의
	// 서버 취소·commit 완료를 증명하지 않으므로 미반영을 가정하지 않습니다.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := lease.Defer(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled defer = %v, want context canceled", err)
	}

	var (
		state     string
		untouched bool
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT slot_state, retry_not_before IS NULL AND last_failure_code IS NULL AND owner_instance = $2
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, lease.proof.JobKey, lease.proof.OwnerInstance).Scan(&state, &untouched); err != nil {
		t.Fatal(err)
	}

	if state != "ACTIVE" || !untouched {
		t.Fatalf("failed defer mutated active lease: state=%s untouched=%t", state, untouched)
	}

	if err := lease.Defer(t.Context(), input); err != nil {
		t.Fatalf("retry after canceled write with original fence: %v", err)
	}
}

func TestDeferSQLRejectsNonCooldownNotBefore(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})

	lease := mustAcquireLease(t, newTestRepository(t, pool), communityJob(), "collector-a")
	proof := lease.Proof()

	var jobKey string

	err := pool.QueryRow(t.Context(), sqlLeaseDefer,
		proof.JobKey, proof.OwnerInstance, proof.FenceEpoch, proof.ProjectionGeneration, proof.ScheduledFor,
		time.Now().UTC().Add(10*time.Minute), string(contract.ErrorCollectionFailed), string(contract.ClassTransient), "failure",
		testRetryBounds.Minimum.Milliseconds(), testRetryBounds.Maximum.Milliseconds(), true,
	).Scan(&jobKey)

	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("non-cooldown not-before SQL = %v, want no rows", err)
	}

	if err := lease.Defer(t.Context(), retryDeadlineInput(t, retryDatabaseClock(t, pool).Add(10*time.Minute), true)); err != nil {
		t.Fatalf("rejected SQL changed active fence: %v", err)
	}
}
