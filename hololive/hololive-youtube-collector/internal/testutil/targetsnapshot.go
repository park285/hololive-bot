package testutil

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
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

	repository, err := joblease.NewRepository(pool, targetSnapshotConfig())
	if err != nil {
		tb.Fatal(err)
	}

	snapshot, err := repository.LoadTargetSnapshot(ctx, targetSnapshotProof(spec, generation), spec, job, 100_000)
	if err != nil {
		tb.Fatal(err)
	}

	return snapshot
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
