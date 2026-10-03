package testutil

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

func TargetSnapshot(
	tb testing.TB,
	pool *pgxpool.Pool,
	spec *joblease.JobSpec,
	job sourceobservation.JobContract,
	subjects map[contract.ObservationKind][]string,
) joblease.TargetSnapshot {
	tb.Helper()

	ctx := tb.Context()

	var generation int64

	if err := pool.QueryRow(ctx, mustTestSQL("insert_projection.sql"), subjectCount(subjects)).Scan(&generation); err != nil {
		tb.Fatal(err)
	}

	insertSnapshotTargets(tb, pool, generation, spec, subjects)

	proof := targetSnapshotProof(spec, generation)
	insertSnapshotLease(tb, pool, spec, job, proof, subjects)

	repository, err := joblease.NewRepository(pool, targetSnapshotConfig())
	if err != nil {
		tb.Fatal(err)
	}

	snapshot, err := repository.LoadTargetSnapshot(ctx, proof, spec, job, 100_000)
	if err != nil {
		tb.Fatal(err)
	}

	return snapshot
}

// insertSnapshotLease는 snapshot이 요구하는 소유 증명과 job membership 범위를 acquire와 같은 의미로 기록한다.
func insertSnapshotLease(
	tb testing.TB,
	pool *pgxpool.Pool,
	spec *joblease.JobSpec,
	job sourceobservation.JobContract,
	proof *contract.LeaseProof,
	subjects map[contract.ObservationKind][]string,
) {
	tb.Helper()

	scope := sourceobservation.MembershipScopeFor(job)

	if _, err := pool.Exec(tb.Context(), mustTestSQL("insert_active_lease.sql"),
		spec.JobKey, spec.Provider, spec.Class, spec.CollectionJobKind, spec.SubjectKey, proof.ProjectionGeneration,
		spec.PollInterval.Milliseconds(), proof.ScheduledFor, proof.FenceEpoch, proof.OwnerInstance,
		scope.Kinds, scope.ExactSubject, scopeTargetCount(scope, spec.SubjectKey, subjects),
	); err != nil {
		tb.Fatal(err)
	}
}

func scopeTargetCount(scope sourceobservation.MembershipScope, subject string, subjects map[contract.ObservationKind][]string) int32 {
	var count int32

	for _, kind := range scope.Kinds {
		for _, value := range subjects[contract.ObservationKind(kind)] {
			if !scope.ExactSubject || value == subject {
				count++
			}
		}
	}

	return count
}

func insertSnapshotTargets(
	tb testing.TB,
	pool *pgxpool.Pool,
	generation int64,
	spec *joblease.JobSpec,
	subjects map[contract.ObservationKind][]string,
) {
	tb.Helper()

	ctx := tb.Context()

	for kind, values := range subjects {
		for _, subject := range values {
			if _, err := pool.Exec(ctx, mustTestSQL("insert_target.sql"), generation, subject, kind, spec.PollInterval.Milliseconds()); err != nil {
				tb.Fatal(err)
			}
		}
	}
}

func targetSnapshotConfig() *joblease.Config {
	return &joblease.Config{
		LeaseTTL: 2 * time.Second, RenewInterval: 100 * time.Millisecond,
		RenewTimeout: 50 * time.Millisecond, DBTimeout: 100 * time.Millisecond, CleanupTimeout: 250 * time.Millisecond,
		MinRetryDelay: 100 * time.Millisecond, MaxRetryDelay: time.Second,
		MinReleaseJitter: 100 * time.Millisecond, MaxReleaseJitter: 200 * time.Millisecond,
		AcquisitionBatch: 10, WorkerCount: 2, QueueCapacity: 4, PollCadence: 100 * time.Millisecond,
	}
}

func targetSnapshotProof(spec *joblease.JobSpec, generation int64) *contract.LeaseProof {
	return &contract.LeaseProof{
		JobKey: spec.JobKey, CollectionJobKind: spec.CollectionJobKind, OwnerInstance: "collector-a",
		FenceEpoch: 1, ProjectionGeneration: generation,
		ScheduledFor: time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC),
	}
}

func subjectCount(subjects map[contract.ObservationKind][]string) int {
	count := 0

	for _, values := range subjects {
		count += len(values)
	}

	return count
}
