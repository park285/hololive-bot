package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func TargetSnapshot(
	t testing.TB,
	spec *joblease.JobSpec,
	job sourceobservation.JobContract,
	subjects map[contract.ObservationKind][]string,
) joblease.TargetSnapshot {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	var generation int64
	if err := pool.QueryRow(ctx, mustTestSQL("insert_projection.sql"), subjectCount(subjects)).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	insertSnapshotTargets(t, ctx, pool, generation, spec, subjects)
	repository, err := joblease.NewRepository(pool, targetSnapshotConfig())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.LoadTargetSnapshot(ctx, targetSnapshotProof(spec, generation), spec, job, 100_000)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func insertSnapshotTargets(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	generation int64,
	spec *joblease.JobSpec,
	subjects map[contract.ObservationKind][]string,
) {
	t.Helper()
	for kind, values := range subjects {
		for _, subject := range values {
			if _, err := pool.Exec(ctx, mustTestSQL("insert_target.sql"), generation, subject, kind, spec.PollInterval.Milliseconds()); err != nil {
				t.Fatal(err)
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
		ScheduledFor: time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC),
	}
}

func subjectCount(subjects map[contract.ObservationKind][]string) int {
	count := 0
	for _, values := range subjects {
		count += len(values)
	}
	return count
}
