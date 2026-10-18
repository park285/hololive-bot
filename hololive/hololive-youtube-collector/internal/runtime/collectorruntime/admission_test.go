package collectorruntime

import (
	"context"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func fixtureCollectorConfig(t *testing.T) collectorconfig.Config {
	t.Helper()

	loaded := settingstest.LoadProfileFixture(t, "hololive", "youtube-collector", "stack-worker-profile-youtube-collector.json")

	profile, err := collectorconfig.DecodeWorkerProfile(loaded)
	if err != nil {
		t.Fatal(err)
	}

	return profile.Config()
}

func runAdmittedSpec(ctx context.Context, t *testing.T, executor *collectionExecutor, spec *joblease.JobSpec) {
	t.Helper()

	job, ok := executor.admit(spec, time.Now(), executor.collector.QueueMaxAge)
	if !ok {
		t.Fatalf("admit test job: %+v", spec)
	}

	executor.runSpec(ctx, &job)
}

func queueTestSpec(key string) joblease.JobSpec {
	return joblease.JobSpec{
		JobKey: key, Provider: contract.ProviderYouTubeJS, CollectionJobKind: testCommunityJobKind,
		Class: testJobClassSubject, SubjectKey: testSubjectKey, PollInterval: time.Minute,
	}
}

func TestAdmissionRejectsInvalidBudgetBeforeQueueOrLease(t *testing.T) {
	for _, name := range []string{"unknown kind", "invalid identity", "invalid cadence", "missing profile", "zero collect", "negative phase", "zero phase", "execution overflow", "deadline overflow", "zero max age"} {
		t.Run(name, func(t *testing.T) {
			scheduler := newLifecycleScheduler(t)

			scheduler.workerTotals = &workercontract.Counters{}

			spec := queueTestSpec(name)
			invalidateAdmissionBudget(scheduler, &spec, name)

			if got := scheduler.enqueue(t.Context(), &spec); got != EnqueueInvalid {
				t.Fatalf("enqueue = %s, want INVALID", got)
			}

			if len(scheduler.queue) != 0 || len(scheduler.queued) != 0 || len(scheduler.queuedAt) != 0 {
				t.Fatal("invalid job consumed queue admission")
			}

			// The fixture repository has no pool: any lease mutation would fail before this assertion.
			totals := scheduler.workerTotals.Snapshot()
			if totals.Admissions.Rejected != 1 || totals.Attempts != (workercontract.AttemptTotals{}) {
				t.Fatalf("totals = %+v", totals)
			}
		})
	}
}

func invalidateAdmissionBudget(scheduler *leaseScheduler, spec *joblease.JobSpec, name string) {
	key := runnerKey{provider: spec.Provider, jobKind: spec.CollectionJobKind}
	registration := scheduler.executor.registry.byKey[key]

	switch name {
	case "unknown kind":
		spec.CollectionJobKind = "unknown"
	case "invalid identity":
		spec.Class = "unknown"
	case "invalid cadence":
		spec.PollInterval = 0
	case "missing profile":
		registration.profile = ExecutionProfile{}
	case "zero collect":
		registration.profile.collectTimeout = 0
	case "negative phase":
		scheduler.executor.collector.DBTimeout = -time.Second
	case "zero phase":
		scheduler.executor.collector.ProviderAdmissionTimeout = 0
	case "execution overflow":
		registration.profile.collectTimeout = time.Duration(math.MaxInt64)
	case "deadline overflow":
		scheduler.queueMaxAge = time.Duration(math.MaxInt64)
	case "zero max age":
		scheduler.queueMaxAge = 0
	}

	scheduler.executor.registry.byKey[key] = registration
}

func TestQueuedDeadlineAndBudgetAreAdmissionOwned(t *testing.T) {
	scheduler := newLifecycleScheduler(t)
	spec := queueTestSpec("immutable")

	if got := scheduler.enqueue(t.Context(), &spec); got != EnqueueAccepted {
		t.Fatal(got)
	}

	job := <-scheduler.queue
	wantDeadline := job.enqueuedAt.Add(job.maxAge).Add(job.budget)

	if job.budget <= 0 || !job.deadline.Equal(wantDeadline) {
		t.Fatalf("budget/deadline = %s/%s, want %s", job.budget, job.deadline, wantDeadline)
	}

	spec.CollectionJobKind = "mutated"
	scheduler.queueMaxAge = time.Nanosecond
	scheduler.executor.collector.DBTimeout = time.Nanosecond
	delete(scheduler.executor.registry.byKey, runnerKey{provider: job.Provider, jobKind: job.CollectionJobKind})

	if got := scheduler.acceptDequeued(t.Context(), &job); got != dequeueRun {
		t.Fatalf("mutated scheduler changed admitted max age: %d", got)
	}

	if job.CollectionJobKind != testCommunityJobKind || job.registration.Runner() == nil || !job.deadline.Equal(wantDeadline) {
		t.Fatal("queued job changed after admission")
	}

	started := job.enqueuedAt.Add(job.maxAge)
	if got := job.executionDeadline(started); !got.Equal(started.Add(job.budget)) {
		t.Fatalf("queue-age equality lost full attempt budget: %s", got)
	}

	if got := job.executionDeadline(started.Add(time.Second)); !got.Equal(job.deadline) {
		t.Fatalf("execution extended immutable deadline: %s", got)
	}

	started = job.enqueuedAt.Add(job.maxAge / 2)
	if got := job.executionDeadline(started); !got.Equal(started.Add(job.budget)) {
		t.Fatalf("fresh execution deadline = %s", got)
	}
}

func TestShutdownDiscardsQueuedJobsExactlyOnce(t *testing.T) {
	scheduler := newLifecycleScheduler(t)

	scheduler.workerTotals = &workercontract.Counters{}

	for _, key := range []string{"dequeued", "queued"} {
		spec := queueTestSpec(key)
		if got := scheduler.enqueue(t.Context(), &spec); got != EnqueueAccepted {
			t.Fatal(got)
		}
	}

	job := <-scheduler.queue
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if scheduler.acceptDequeued(ctx, &job) != dequeueStop {
		t.Fatal("shutdown dequeued job was runnable")
	}

	scheduler.drainQueue()
	scheduler.drainQueue()

	discarded := scheduler.workerTotals.Snapshot().Discarded
	if discarded.Shutdown != 2 || discarded.Stale != 0 {
		t.Fatalf("discarded = %+v", discarded)
	}
}

func TestQueueMaxAgeEqualityRetainsFullAttemptBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scheduler := newLifecycleScheduler(t)
		spec := queueTestSpec("age-boundary")

		if result := scheduler.enqueue(t.Context(), &spec); result != EnqueueAccepted {
			t.Fatal(result)
		}

		time.Sleep(scheduler.queueMaxAge)

		job, ok := scheduler.nextSpec(t.Context())
		if !ok || time.Until(job.executionDeadline(time.Now())) != job.budget {
			t.Fatal("queue-age equality discarded or shortened an admitted job")
		}
	})
}
