package collectorruntime

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-collector/internal/runtime/collection"
	"github.com/kapu/hololive-collector/internal/runtime/joblease"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func admitShortTestJob(t *testing.T, executor *collectionExecutor, spec *joblease.JobSpec) queuedJob {
	t.Helper()

	executor.collector.DBTimeout = 200 * time.Millisecond
	executor.collector.ProviderAdmissionTimeout = 200 * time.Millisecond
	executor.collector.PublishTimeout = 200 * time.Millisecond

	profile, err := NewExecutionProfile(1, 500*time.Millisecond, 0, 1, 500*time.Millisecond)
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

	config := dbtest.NewPool(t).Config()
	synctest.Test(t, func(t *testing.T) {
		executor, spec := newExecutorFixtureOnPool(t, admissionPool(t, config), runner, &fatal)
		job := admitShortTestJob(t, executor, spec)

		executor.collector.ProviderAdmissionTimeout = 10 * time.Second

		gate := make(chan struct{}, 1)
		gate <- struct{}{}

		executor.gates[spec.Provider] = gate

		started := time.Now()

		executor.runSpec(t.Context(), &job)

		if elapsed := time.Since(started); elapsed != job.budget {
			t.Fatalf("provider wait elapsed %s, admitted budget %s", elapsed, job.budget)
		}

		if calls.Load() != 0 || len(gate) != 1 {
			t.Fatalf("unowned provider gate changed: calls=%d depth=%d", calls.Load(), len(gate))
		}

		if totals := executor.workerTotals.Snapshot().Attempts; totals.Timeout != 1 || totals.Success != 0 {
			t.Fatalf("attempt totals = %+v", totals)
		}
	})

	if len(fatal) != 0 {
		t.Fatalf("fatal = %v", fatal)
	}
}

func TestAdmittedBudgetBoundsBlockedPublishAndPreservesDetachedCleanup(t *testing.T) {
	withCollectorClock(t, checkBlockedPublishDeadline)
}

type lockObserver struct {
	expired <-chan struct{}
	scan    func(dest any, query string, args ...any) error
}

func withCollectorClock(t *testing.T, run func(*testing.T, *lockObserver, *pgxpool.Pool)) {
	t.Helper()

	pool := dbtest.NewPool(t)

	guard, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	queries := make(chan func())
	defer close(queries)

	go func() {
		for query := range queries {
			query()
		}
	}()

	results := make(chan error)
	observer := &lockObserver{
		expired: guard.Done(),
		scan: func(dest any, query string, args ...any) error {
			queries <- func() { results <- pool.QueryRow(guard, query, args...).Scan(dest) }

			return <-results
		},
	}

	synctest.Test(t, func(t *testing.T) {
		run(t, observer, admissionPool(t, pool.Config()))
	})
}

func admissionPool(t *testing.T, config *pgxpool.Config) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func checkBlockedPublishDeadline(t *testing.T, observer *lockObserver, pool *pgxpool.Pool) {
	t.Helper()

	var fatal []error

	executor, entered, publish := blockedPublishDeadlineFixture(t, pool, &fatal)

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

	awaitPublishStage(t, entered, done, observer.expired, "collection did not start")

	held := lockLeaseRow(t, pool, spec.JobKey)
	parked, releaseRead := installPublishReadBarrier(t, executor, pool)

	defer releaseRead()

	close(publish)

	writer := held.waitForBlockedWriter(t, observer)

	assertBlockedPublisher(t, observer, writer)

	awaitPublishStage(t, parked, done, observer.expired, "publisher did not reach the prepared Execute read barrier")

	<-time.After(job.budget)
	releaseRead()
	waitForPublishCancellation(t, observer, held, writer)
	held.release(t)

	awaitPublishStage(t, done, nil, time.After(3*time.Second), "blocked publish escaped admitted deadline and bounded cleanup")

	assertDetachedPublishCleanup(t, executor, pool, spec, fatal)
}

const publisherName = "admission-deadline-publisher"

func installPublishReadBarrier(t *testing.T, executor *collectionExecutor, pool *pgxpool.Pool) (<-chan struct{}, func()) {
	t.Helper()

	resume := make(chan struct{})
	parked := make(chan struct{})
	parkRead := sync.OnceFunc(func() {
		close(parked)
		<-resume
	})

	releaseRead := sync.OnceFunc(func() { close(resume) })

	config := pool.Config()

	config.ConnConfig.RuntimeParams["application_name"] = publisherName
	config.ConnConfig.AfterNetConnect = func(_ context.Context, _ *pgconn.Config, conn net.Conn) (net.Conn, error) {
		return &publishFenceConn{Conn: conn, parkRead: parkRead}, nil
	}
	executor.publisher = NewPublisher(admissionPool(t, config))

	return parked, releaseRead
}

func awaitPublishStage[T any](t *testing.T, reached, ended <-chan struct{}, expired <-chan T, expectation string) {
	t.Helper()

	select {
	case <-reached:
		return
	case <-ended:
	case <-expired:
	}

	select {
	case <-reached:
	default:
		t.Fatal(expectation)
	}
}

func assertBlockedPublisher(t *testing.T, observer *lockObserver, writer int32) {
	t.Helper()

	var name string

	if err := observer.scan(&name, `SELECT application_name FROM pg_stat_activity WHERE pid = $1`, writer); err != nil || name != publisherName {
		t.Fatalf("blocked writer = %q, want %q, error = %v", name, publisherName, err)
	}
}

type publishFenceConn struct {
	net.Conn

	parkRead func()
	fence    atomic.Bool
}

func (c *publishFenceConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err != nil {
		return n, fmt.Errorf("write publish request: %w", err)
	}

	if sendsPreparedExecute(p[:n]) {
		c.fence.Store(true)
	}

	return n, nil
}

func sendsPreparedExecute(p []byte) bool {
	if len(p) == 0 || p[0] != 'B' {
		return false
	}

	messages := pgproto3.NewBackend(bytes.NewReader(p), nil)

	for {
		message, err := messages.Receive()
		if err != nil {
			return false
		}

		if _, ok := message.(*pgproto3.Execute); ok {
			return true
		}
	}
}

func (c *publishFenceConn) Read(p []byte) (int, error) {
	if c.fence.Swap(false) {
		c.parkRead()
	}

	n, err := c.Conn.Read(p)
	if err != nil {
		return n, fmt.Errorf("read publish response: %w", err)
	}

	return n, nil
}

func waitForPublishCancellation(t *testing.T, observer *lockObserver, held *heldLeaseRow, writer int32) {
	t.Helper()

	for {
		var blocked bool

		if err := observer.scan(&blocked, `SELECT $1::int = ANY(pg_blocking_pids($2))`, held.pid, writer); err != nil {
			t.Fatalf("publish did not leave the lock queue within its admitted budget: %v", err)
		}

		if !blocked {
			return
		}
	}
}

func blockedPublishDeadlineFixture(t *testing.T, pool *pgxpool.Pool, fatal *[]error) (*collectionExecutor, <-chan struct{}, chan<- struct{}) {
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

	executor := newGateExecutor(t, pool, runner, 1, fatal, testSubjectKey)

	executor.workerTracker = workercontract.NewExecutorTracker()
	executor.workerTotals = &workercontract.Counters{}

	return executor, entered, publish
}

func assertDetachedPublishCleanup(t *testing.T, executor *collectionExecutor, pool *pgxpool.Pool, spec *joblease.JobSpec, fatal []error) {
	t.Helper()

	assertSubjectObservations(t, pool, testSubjectKey, 0)

	var released bool

	if err := pool.QueryRow(t.Context(), `
		SELECT slot_state = 'DEFERRED' AND owner_instance IS NULL AND lease_expires_at IS NULL
			AND last_error_code = $2 FROM youtube_collection_job_leases WHERE job_key = $1
	`, spec.JobKey, joblease.ReleaseShutdown).Scan(&released); err != nil || !released {
		t.Fatalf("detached shutdown release = %t, error = %v", released, err)
	}

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
