package collectorruntime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func admitShortTestJob(t *testing.T, executor *collectionExecutor, spec *joblease.JobSpec) queuedJob {
	t.Helper()

	executor.collector.DBTimeout = 200 * time.Millisecond
	executor.collector.ProviderAdmissionTimeout = 200 * time.Millisecond
	executor.collector.PublishTimeout = 200 * time.Millisecond

	profile, err := NewExecutionProfile(1, 10*time.Millisecond, 0, 1, 10*time.Millisecond, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	key := runnerKey{provider: spec.Provider, jobKind: spec.CollectionJobKind}
	registration := executor.registry.byKey[key]

	registration.profile = profile
	executor.registry.byKey[key] = registration

	job, ok := executor.admit(spec, time.Now(), executor.collector.QueueMaxAge)

	if !ok {
		t.Fatal("finite test budget rejected")
	}

	return job
}

func TestAdmittedBudgetBoundsProviderWaitAfterConfigurationChanges(t *testing.T) {
	var (
		fatal []error
		calls atomic.Int32
	)

	runner := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

	runner.collect = func(context.Context, *collection.RunInput) (collection.CollectResult, error) {
		calls.Add(1)

		return collection.CollectResult{}, nil
	}

	executor, spec := newExecutorFixture(t, runner, &fatal)
	job := admitShortTestJob(t, executor, spec)

	executor.collector.ProviderAdmissionTimeout = 10 * time.Second

	gate := make(chan struct{}, 1)

	gate <- struct{}{}

	executor.gates[spec.Provider] = gate

	started := time.Now()

	executor.runSpec(t.Context(), &job)

	elapsed := time.Since(started)
	if elapsed < job.budget || elapsed > job.budget+3*time.Second {
		t.Fatalf("provider wait elapsed %s, admitted budget %s", elapsed, job.budget)
	}

	if calls.Load() != 0 || len(gate) != 1 {
		t.Fatalf("unowned provider gate changed: calls=%d depth=%d", calls.Load(), len(gate))
	}

	if totals := executor.workerTotals.Snapshot().Attempts; totals.Timeout != 1 || totals.Success != 0 {
		t.Fatalf("attempt totals = %+v", totals)
	}

	if len(fatal) != 0 {
		t.Fatalf("fatal = %v", fatal)
	}
}

func TestAdmittedBudgetBoundsBlockedPublishAndPreservesDetachedCleanup(t *testing.T) {
	var fatal []error

	executor, pool, entered, publish := blockedPublishDeadlineFixture(t, &fatal)
	spec := gateMetadataSpec(testSubjectKey)
	job := admitShortTestJob(t, executor, spec)

	executor.collector.PublishTimeout = 10 * time.Second

	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	done := make(chan struct{})

	go func() {
		defer close(done)

		executor.runSpec(ctx, &job)
	}()

	select {
	case <-entered:
	case <-done:
		t.Fatal("attempt ended before collect")
	case <-time.After(5 * time.Second):
		t.Fatal("collect did not start")
	}

	held := lockLeaseRow(t, pool, spec.JobKey)

	close(publish)
	held.waitForBlockedWriter(t)

	// 총 시도 기한 후에도 cleanup의 독립 예산 안에서 잠금을 풀어 terminal 정리를 허용한다.
	remaining := time.Until(job.enqueuedAt.Add(job.budget))
	if remaining > 0 {
		time.Sleep(remaining)
	}

	time.Sleep(50 * time.Millisecond)
	held.release(t)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("blocked publish escaped admitted deadline and bounded cleanup")
	}

	assertDetachedPublishCleanup(t, executor, pool, spec, fatal)
}

func blockedPublishDeadlineFixture(t *testing.T, fatal *[]error) (*collectionExecutor, *pgxpool.Pool, <-chan struct{}, chan<- struct{}) {
	t.Helper()

	entered := make(chan struct{})
	publish := make(chan struct{})
	runner := stubJob(contract.ProviderYouTubeJS, gateMetadataJobKind)

	runner.collect = func(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
		close(entered)

		select {
		case <-publish:
			return collectYouTubeJSMetadata(ctx, input)
		case <-ctx.Done():
			return collection.CollectResult{}, ctx.Err()
		}
	}

	executor, pool := newGateFixture(t, runner, 1, fatal, testSubjectKey)
	// 잠긴 publish를 renewal 실패와 혼동하지 않도록 갱신 시각은 이 시도 뒤에 둔다.
	leaseConfig := runtimeLeaseConfig()

	leaseConfig.LeaseTTL = 30 * time.Second
	leaseConfig.RenewInterval = 10 * time.Second

	var err error

	executor.repository, err = joblease.NewRepository(pool, &leaseConfig)
	if err != nil {
		t.Fatal(err)
	}

	executor.workerTracker = workercontract.NewExecutorTracker()
	executor.workerTotals = &workercontract.Counters{}

	return executor, pool, entered, publish
}

func assertDetachedPublishCleanup(t *testing.T, executor *collectionExecutor, pool *pgxpool.Pool, spec *joblease.JobSpec, fatal []error) {
	t.Helper()

	assertSubjectObservations(t, pool, testSubjectKey, 0)

	if totals := executor.workerTotals.Snapshot().Attempts; totals.Success != 0 || totals.Timeout+totals.Failed+totals.Canceled != 1 {
		t.Fatalf("attempt totals = %+v", totals)
	}

	if len(executor.gates[spec.Provider]) != 0 {
		t.Fatal("joined collector retained provider gate")
	}

	if len(fatal) != 0 {
		t.Fatalf("fatal = %v", fatal)
	}
}
