package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

const testJobClassSubject = "SUBJECT"

type countedTerminalLease struct {
	joblease.Lease

	defers    int
	completes int
}

func (l *countedTerminalLease) Defer(ctx context.Context, input collection.DeferCollectionInput) error {
	l.defers++
	if err := l.Lease.Defer(ctx, input); err != nil {
		return fmt.Errorf("defer counted lease: %w", err)
	}

	return nil
}

func (l *countedTerminalLease) CompleteCurrent(ctx context.Context) error {
	l.completes++
	if err := l.Lease.CompleteCurrent(ctx); err != nil {
		return fmt.Errorf("complete counted lease: %w", err)
	}

	return nil
}

func newExecutorFixture(t *testing.T, runner collection.JobRunner, fatal *[]error) (*collectionExecutor, *joblease.JobSpec) {
	t.Helper()

	pool := dbtest.NewPool(t)
	seedRuntimeCommunityTarget(t, pool)

	executor := newRunErrorExecutor(fatal)

	executor.retryBounds = testRetryBounds

	config := runtimeLeaseConfig()

	var err error

	executor.repository, err = joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	executor.registry, err = newTestRegistry(withOverride(runner)...)
	if err != nil {
		t.Fatal(err)
	}

	executor.publisher = NewPublisher(pool)
	executor.owner = testOwnerInstance
	executor.gates = defaultProviderGates()
	executor.readiness = &readinessTracker{}
	executor.workerTracker = workercontract.NewExecutorTracker()
	executor.workerTotals = &workercontract.Counters{}

	return executor, &joblease.JobSpec{
		JobKey: "collector:youtubejs:community_collect:UC_TEST", Provider: contract.ProviderYouTubeJS, Class: testJobClassSubject,
		CollectionJobKind: testCommunityJobKind, SubjectKey: testSubjectKey, PollInterval: time.Minute,
	}
}

func TestResultInvariantRecordsFailedAttemptOnce(t *testing.T) {
	var fatal []error

	runner := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

	runner.collect = func(context.Context, *collection.RunInput) (collection.CollectResult, error) {
		return collection.CollectResult{}, nil
	}

	executor, spec := newExecutorFixture(t, runner, &fatal)
	metrics := prometheus.NewPedanticRegistry()

	executor.metrics = NewMetrics(metrics)

	lease, err := executor.acquireLease(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}

	counted := &countedTerminalLease{Lease: lease}
	registration, _ := executor.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	_, runErr := executor.runAcquired(t.Context(), registration, spec, counted)

	if failure, ok := errors.AsType[*FatalRuntimeError](runErr); !ok || failure.Phase != "result_validation" {
		t.Fatalf("runAcquired error = %v, want result_validation FatalRuntimeError", runErr)
	}

	if counted.defers != 1 || counted.completes != 0 {
		t.Fatalf("terminal calls defer=%d complete=%d, want 1/0", counted.defers, counted.completes)
	}

	if totals := executor.workerTotals.Snapshot().Attempts; totals.Failed != 1 || totals.Success != 0 {
		t.Fatalf("worker attempts = %+v, want one failure and no success", totals)
	}

	if executor.readiness.Snapshot().collectionSuccess {
		t.Fatal("invalid result recorded readiness success")
	}

	labels := map[string]string{labelProvider: string(spec.Provider), labelKind: spec.CollectionJobKind, "result": resultFailed}
	if got := metricValue(t, metrics, "youtube_collection_attempts_total", labels); got != 1 {
		t.Fatalf("failed attempts = %v, want 1", got)
	}

	labels["result"] = resultSuccess
	if got := metricValue(t, metrics, "youtube_collection_attempts_total", labels); got != 0 {
		t.Fatalf("success attempts = %v, want 0", got)
	}

	if len(fatal) != 1 {
		t.Fatalf("fatal reports = %v, want one", fatal)
	}

	failure, ok := errors.AsType[*FatalRuntimeError](fatal[0])
	if !ok || failure.Phase != "result_validation" {
		t.Fatalf("fatal = %v, want result_validation", fatal)
	}
}

// 계약 밖 failure tuple은 기본 class로 수리하지 않는다. 미분류 Internal로 지연 처리하고 위반 counter로만 드러낸다
// (stack-audit 2026-09-26 T11 holo-collector-failure-class-repair).
func TestInvalidFailureTupleDefersAndCountsViolation(t *testing.T) {
	var fatal []error

	runner := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

	runner.collect = func(context.Context, *collection.RunInput) (collection.CollectResult, error) {
		return collection.CollectResult{}, collecterr.New(collecterr.Failed, collecterr.ClassTimeout, "impossible tuple")
	}

	executor, spec := newExecutorFixture(t, runner, &fatal)
	metrics := prometheus.NewPedanticRegistry()

	executor.metrics = NewMetrics(metrics)

	lease, err := executor.acquireLease(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}

	counted := &countedTerminalLease{Lease: lease}
	registration, _ := executor.registry.Lookup(spec.Provider, spec.CollectionJobKind)

	if _, runErr := executor.runAcquired(t.Context(), registration, spec, counted); !errors.Is(runErr, collecterr.ErrInvalidFailureTuple) {
		t.Fatalf("runAcquired error = %v, want invalid failure tuple", runErr)
	}

	if counted.defers != 1 || counted.completes != 0 {
		t.Fatalf("terminal calls defer=%d complete=%d, want 1/0", counted.defers, counted.completes)
	}

	if len(fatal) != 0 {
		t.Fatalf("invalid failure tuple was promoted to fatal: %v", fatal)
	}

	labels := map[string]string{labelProvider: string(spec.Provider), labelKind: spec.CollectionJobKind}
	if got := metricValue(t, metrics, "youtube_collection_invalid_failure_tuple_total", labels); got != 1 {
		t.Fatalf("invalid failure tuple violations = %v, want 1", got)
	}

	labels["result"] = resultFailed
	if got := metricValue(t, metrics, "youtube_collection_attempts_total", labels); got != 1 {
		t.Fatalf("failed attempts = %v, want 1", got)
	}
}

func TestCollectDeadlinePreservesClassifiedRunnerFailure(t *testing.T) {
	for _, cause := range []error{
		collecterr.New(collecterr.Internal, collecterr.ClassInternal, "runner invariant"),
		collecterr.New(collecterr.HelperProtocolMismatch, collecterr.ClassProtocol, "runner protocol"),
	} {
		t.Run(string(collecterr.ClassOf(cause)), func(t *testing.T) {
			var fatal []error

			runner := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

			runner.collect = func(ctx context.Context, _ *collection.RunInput) (collection.CollectResult, error) {
				<-ctx.Done()

				return collection.CollectResult{}, cause
			}

			executor, spec := newExecutorFixture(t, runner, &fatal)

			lease, err := executor.acquireLease(t.Context(), spec)
			if err != nil {
				t.Fatal(err)
			}

			registration, _ := executor.registry.Lookup(spec.Provider, spec.CollectionJobKind)

			registration.profile.collectTimeout = time.Millisecond

			proof := lease.Proof()

			_, err = executor.collectAndPublish(t.Context(), registration, spec, lease, &proof)

			if !errors.Is(err, cause) || !errors.Is(err, context.DeadlineExceeded) || !fatalCollectionError(err) {
				t.Fatalf("collect = %v, want classified cause and deadline", err)
			}

			executor.handleRunError(t.Context(), lease, spec, &proof, err)

			if len(fatal) != 1 || !errors.Is(fatal[0], cause) || !errors.Is(fatal[0], context.DeadlineExceeded) {
				t.Fatalf("fatal reports = %v, want both causes", fatal)
			}
		})
	}
}

func TestSchedulerReusesExecutorAndPreservesFatalCauses(t *testing.T) {
	scheduler := newLifecycleScheduler(t)
	executor := scheduler.executor

	executor.gates = defaultProviderGates()

	if err := scheduler.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	cause := collecterr.New(collecterr.Internal, collecterr.ClassInternal, "executor invariant")
	executor.reportFatal(errors.Join(cause, context.DeadlineExceeded))

	select {
	case err := <-scheduler.Fatal():
		if !errors.Is(err, cause) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("fatal = %v, want both causes", err)
		}
	case <-time.After(time.Second):
		t.Fatal("executor fatal was not delivered")
	}

	if err := scheduler.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if scheduler.executor != executor || scheduler.Snapshot().State != SchedulerStopped {
		t.Fatal("executor lifetime diverged from scheduler")
	}
}

type runnerFailureCase struct {
	name         string
	run          func()
	wantFatal    bool
	waitDeadline bool
	wantCause    error
}

func TestRunnerPanicsAreFatalButReturnedErrorsRemainTransient(t *testing.T) {
	cause := errors.New("runner error panic")
	for _, test := range []runnerFailureCase{
		{name: "string panic", run: func() { panic("runner panic") }, wantFatal: true},
		{name: "error panic", run: func() { panic(cause) }, wantFatal: true, wantCause: cause},
		{name: "runtime panic", run: func() { denominator := 0; _ = 1 / denominator }, wantFatal: true},
		{name: "panic after deadline", run: func() { panic(cause) }, wantFatal: true, wantCause: cause, waitDeadline: true},
		{name: "returned raw error", run: func() {}},
	} {
		t.Run(test.name, func(t *testing.T) { checkRunnerFailure(t, test) })
	}
}

func checkRunnerFailure(t *testing.T, test runnerFailureCase) {
	t.Helper()

	var fatal []error

	runner := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

	runner.collect = func(ctx context.Context, _ *collection.RunInput) (collection.CollectResult, error) {
		if test.waitDeadline {
			<-ctx.Done()
		}

		test.run()

		return collection.CollectResult{}, errors.New("returned provider error")
	}

	executor, spec := newExecutorFixture(t, runner, &fatal)

	lease, err := executor.acquireLease(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}

	counted := &countedTerminalLease{Lease: lease}
	registration, _ := executor.registry.Lookup(spec.Provider, spec.CollectionJobKind)

	if test.waitDeadline {
		registration.profile.collectTimeout = time.Millisecond
	}

	_, runErr := executor.runAcquired(t.Context(), registration, spec, counted)
	if runErr == nil || fatalCollectionError(runErr) != test.wantFatal {
		t.Fatalf("runAcquired error = %v, want failure with fatal=%t", runErr, test.wantFatal)
	}

	if test.wantCause != nil && !errors.Is(runErr, test.wantCause) {
		t.Fatalf("runAcquired error lost panic cause: %v", runErr)
	}

	if counted.defers != 1 || counted.completes != 0 {
		t.Fatalf("terminal calls = %d/%d, want one defer", counted.defers, counted.completes)
	}

	if totals := executor.workerTotals.Snapshot().Attempts; totals.Failed != 1 || totals.Success != 0 {
		t.Fatalf("attempts = %+v, want one failure", totals)
	}

	if executor.readiness.Snapshot().collectionSuccess {
		t.Fatal("runner failure recorded readiness success")
	}

	checkRunnerFatal(t, test, fatal)
}

func checkRunnerFatal(t *testing.T, test runnerFailureCase, fatal []error) {
	t.Helper()

	if !test.wantFatal {
		if len(fatal) != 0 {
			t.Fatalf("returned error was promoted: %v", fatal)
		}

		return
	}

	if len(fatal) != 1 || !fatalCollectionError(fatal[0]) {
		t.Fatalf("fatal = %v, want one classified panic", fatal)
	}

	if test.wantCause != nil && !errors.Is(fatal[0], test.wantCause) {
		t.Fatalf("panic cause lost: %v", fatal[0])
	}

	if test.waitDeadline && !errors.Is(fatal[0], context.DeadlineExceeded) {
		t.Fatalf("deadline cause lost: %v", fatal[0])
	}
}
