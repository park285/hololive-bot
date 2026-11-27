package collectorruntime

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-collector/internal/runtime/collection"
	"github.com/kapu/hololive-collector/internal/runtime/joblease"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	gateMetadataJobKind = "youtubejs_channel_metadata"
	gateSecondSubject   = "UC_TEST_SECOND"
)

func TestProviderGateReleasesBeforeDatabaseBlockedPublish(t *testing.T) {
	withCollectorClock(t, checkProviderGateDuringPublish)
}

func checkProviderGateDuringPublish(t *testing.T, observer *lockObserver, pool *pgxpool.Pool) {
	t.Helper()

	ctx := t.Context()

	var (
		fatal     []error
		collected atomic.Int32
	)

	executor := newGateExecutor(t, pool, countingMetadataRunner(&collected), 1, &fatal, testSubjectKey, gateSecondSubject)

	executor.collector.PublishTimeout = 15 * time.Second

	gate := executor.gates[contract.ProviderYouTubeJS]

	first := gateMetadataSpec(testSubjectKey)
	firstLease := mustAcquireGateLease(t, executor, first)
	held := lockLeaseRow(t, pool, first.JobKey)
	firstDone := startCollectAndPublish(ctx, executor, first, firstLease)

	held.waitForBlockedWriter(t, observer)

	if depth := len(gate); depth != 0 {
		t.Fatalf("gate depth while publish is blocked = %d, want 0", depth)
	}

	second := gateMetadataSpec(gateSecondSubject)
	secondDone := startCollectAndPublish(ctx, executor, second, mustAcquireGateLease(t, executor, second))

	if err := awaitRun(t, observer.expired, secondDone, "second same-provider job while the first publish is blocked"); err != nil {
		t.Fatalf("second same-provider job = %v, want published while first publish is blocked", err)
	}

	assertRunPending(t, firstDone)
	assertSubjectObservations(t, pool, testSubjectKey, 0)
	held.release(t)

	if err := awaitRun(t, observer.expired, firstDone, "first publish after the lease row is released"); err != nil {
		t.Fatalf("first publish after unlock = %v", err)
	}

	if collected.Load() != 2 || len(gate) != 0 || len(fatal) != 0 {
		t.Fatalf("collected=%d gate depth=%d fatal=%v, want 2/0/none", collected.Load(), len(gate), fatal)
	}

	assertSubjectObservations(t, pool, testSubjectKey, 2)
	assertSubjectObservations(t, pool, gateSecondSubject, 2)
}

type heldLeaseRow struct {
	tx  pgx.Tx
	pid int32
}

func lockLeaseRow(t *testing.T, pool *pgxpool.Pool, jobKey string) *heldLeaseRow {
	t.Helper()

	ctx := t.Context()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()

		if rollbackErr := pgxdb.Rollback(cleanupCtx, tx); rollbackErr != nil {
			t.Errorf("release held lease row: %v", rollbackErr)
		}
	})

	if _, err = tx.Exec(ctx, `SET LOCAL idle_in_transaction_session_timeout = '30s'`); err != nil {
		t.Fatalf("set held lease lock watchdog: %v", err)
	}

	var pid int32

	err = tx.QueryRow(ctx, `
		SELECT pg_backend_pid() FROM youtube_collection_job_leases WHERE job_key = $1 FOR UPDATE
	`, jobKey).Scan(&pid)
	if err != nil {
		t.Fatal(err)
	}

	return &heldLeaseRow{tx: tx, pid: pid}
}

func (h *heldLeaseRow) waitForBlockedWriter(t *testing.T, observer *lockObserver) int32 {
	t.Helper()

	for {
		var pid int32

		err := observer.scan(&pid, `
			SELECT pid FROM pg_stat_activity WHERE $1::int = ANY(pg_blocking_pids(pid))
		`, h.pid)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}

		if err != nil {
			t.Fatalf("wait for publish to block on held lease: %v", err)
		}

		return pid
	}
}

func (h *heldLeaseRow) release(t *testing.T) {
	t.Helper()

	if err := pgxdb.Rollback(t.Context(), h.tx); err != nil {
		t.Fatalf("release held lease row: %v", err)
	}
}

func countingMetadataRunner(collected *atomic.Int32) *stubRunner {
	runner := stubJob(contract.ProviderYouTubeJS, gateMetadataJobKind)

	runner.collect = func(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
		collected.Add(1)

		return collectYouTubeJSMetadata(ctx, input)
	}

	return runner
}

func mustAcquireGateLease(t *testing.T, executor *collectionExecutor, spec *joblease.JobSpec) joblease.Lease {
	t.Helper()

	lease, err := executor.acquireLease(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}

	return lease
}

func awaitRun(t *testing.T, expired <-chan struct{}, done <-chan error, name string) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-expired:
		select {
		case err := <-done:
			return err
		default:
			t.Fatalf("%s did not finish", name)

			return nil
		}
	}
}

func assertRunPending(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		t.Fatalf("first publish finished while its lease row was held: %v", err)
	default:
	}
}

type gateReleaseCase struct {
	name        string
	collect     func(context.Context, *collection.RunInput, context.CancelFunc) (collection.CollectResult, error)
	prepare     func(*collectionExecutor, *RegisteredRunner)
	wantErr     bool
	wantCollect bool
}

func TestProviderGateReleasesOwnSlotExactlyOnce(t *testing.T) {
	for _, test := range []gateReleaseCase{
		{
			name: "published", wantCollect: true,
			collect: func(ctx context.Context, input *collection.RunInput, _ context.CancelFunc) (collection.CollectResult, error) {
				return collectYouTubeJSMetadata(ctx, input)
			},
		},
		{
			name: "runner panic", wantErr: true, wantCollect: true,
			collect: func(context.Context, *collection.RunInput, context.CancelFunc) (collection.CollectResult, error) {
				panic("runner panic")
			},
		},
		{
			name: "run input rejected", wantErr: true,
			prepare: func(executor *collectionExecutor, _ *RegisteredRunner) { executor.collector.MaxPages = 0 },
		},
		{
			name: "collect deadline", wantErr: true, wantCollect: true,
			prepare: func(_ *collectionExecutor, registration *RegisteredRunner) {
				registration.profile.collectTimeout = time.Millisecond
			},
			collect: func(ctx context.Context, _ *collection.RunInput, _ context.CancelFunc) (collection.CollectResult, error) {
				<-ctx.Done()

				return collection.CollectResult{}, ctx.Err()
			},
		},
		{
			name: "parent canceled after admission", wantErr: true, wantCollect: true,
			collect: func(ctx context.Context, _ *collection.RunInput, cancel context.CancelFunc) (collection.CollectResult, error) {
				cancel()

				return collection.CollectResult{}, ctx.Err()
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) { checkGateRelease(t, test) })
	}
}

func checkGateRelease(t *testing.T, test gateReleaseCase) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var (
		fatal     []error
		collected atomic.Int32
	)

	runner := stubJob(contract.ProviderYouTubeJS, gateMetadataJobKind)

	runner.collect = func(runCtx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
		collected.Add(1)

		if test.collect == nil {
			return collection.CollectResult{}, errors.New("collector must not run")
		}

		return test.collect(runCtx, input, cancel)
	}

	executor, _ := newGateFixture(t, runner, 2, &fatal, testSubjectKey)
	gate := executor.gates[contract.ProviderYouTubeJS]

	gate <- struct{}{}

	spec := gateMetadataSpec(testSubjectKey)

	lease, err := executor.acquireLease(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	registration, ok := executor.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	if !ok {
		t.Fatal("metadata runner is not registered")
	}

	if test.prepare != nil {
		test.prepare(executor, &registration)
	}

	proof := lease.Proof()

	err = executor.collectAndPublish(ctx, registration, spec, lease, &proof)

	if (err != nil) != test.wantErr {
		t.Fatalf("collectAndPublish error = %v, want error %t", err, test.wantErr)
	}

	if ran := collected.Load() == 1; ran != test.wantCollect {
		t.Fatalf("collector ran = %t, want %t", ran, test.wantCollect)
	}

	if depth := len(gate); depth != 1 {
		t.Fatalf("gate depth = %d, want only the other job's slot", depth)
	}
}

func TestDeferFailedRunKeepsLeaseActiveForNonDeferableDiagnostic(t *testing.T) {
	ctx := t.Context()

	var (
		fatal  []error
		logged bytes.Buffer
	)

	runner := stubJob(contract.ProviderYouTubeJS, gateMetadataJobKind)
	executor, pool := newGateFixture(t, runner, 1, &fatal, testSubjectKey)

	executor.logger = slog.New(slog.NewTextHandler(&logged, nil))

	spec := gateMetadataSpec(testSubjectKey)
	lease := mustAcquireGateLease(t, executor, spec)

	proof := lease.Proof()
	cause := collecterr.New(contract.ErrorObservationCollision, contract.ClassDataContract, "observation collision")

	executor.deferFailedRun(ctx, lease, spec, &proof, cause)

	if len(fatal) != 0 {
		t.Fatalf("non-deferable diagnostic was promoted to fatal: %v", fatal)
	}

	var (
		state     string
		fence     int64
		errorCode *string
	)

	err := pool.QueryRow(ctx, `
		SELECT slot_state, fence_epoch, last_error_code FROM youtube_collection_job_leases WHERE job_key = $1
	`, spec.JobKey).Scan(&state, &fence, &errorCode)
	if err != nil {
		t.Fatal(err)
	}

	if state != "ACTIVE" || fence != proof.FenceEpoch || errorCode != nil {
		t.Fatalf("lease row = state %s fence %d error %v, want untouched ACTIVE fence %d", state, fence, errorCode, proof.FenceEpoch)
	}

	logs := logged.String()
	if !strings.Contains(logs, "phase=defer") || !strings.Contains(logs, "error_code="+string(collecterr.OperationLeaseDeferFailed)) ||
		strings.Contains(logs, "phase=collect") {
		t.Fatalf("defer failure log = %s", logs)
	}

	if err = lease.CompleteCurrent(ctx); err != nil {
		t.Fatalf("lease fence was lost after rejected defer input: %v", err)
	}
}

func newGateFixture(
	t *testing.T,
	runner collection.JobRunner,
	capacity int,
	fatal *[]error,
	subjects ...string,
) (*collectionExecutor, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)

	return newGateExecutor(t, pool, runner, capacity, fatal, subjects...), pool
}

func newGateExecutor(t *testing.T, pool *pgxpool.Pool, runner collection.JobRunner, capacity int, fatal *[]error, subjects ...string) *collectionExecutor {
	t.Helper()

	seeds := make([]leaseSeed, 0, 2*len(subjects))

	for _, subject := range subjects {
		seeds = append(seeds, leaseSeed{subject, contract.KindChannelProfile}, leaseSeed{subject, contract.KindChannelPhoto})
	}

	seedRuntimeTargets(t, pool, seeds)

	executor := newRunErrorExecutor(t, fatal)

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
	executor.gates = map[contract.Provider]chan struct{}{contract.ProviderYouTubeJS: make(chan struct{}, capacity)}
	executor.readiness = &readinessTracker{}

	return executor
}

func gateMetadataSpec(subject string) *joblease.JobSpec {
	return &joblease.JobSpec{
		JobKey:   "collector:youtubejs:" + gateMetadataJobKind + ":" + subject,
		Provider: contract.ProviderYouTubeJS, Class: testJobClassSubject,
		CollectionJobKind: gateMetadataJobKind, SubjectKey: subject, PollInterval: time.Minute,
	}
}

func startCollectAndPublish(ctx context.Context, executor *collectionExecutor, spec *joblease.JobSpec, lease joblease.Lease) <-chan error {
	done := make(chan error, 1)
	registration, ok := executor.registry.Lookup(spec.Provider, spec.CollectionJobKind)

	if !ok {
		done <- errors.New("metadata runner is not registered")

		return done
	}

	proof := lease.Proof()

	go func() { done <- executor.collectAndPublish(ctx, registration, spec, lease, &proof) }()

	return done
}

func assertSubjectObservations(t *testing.T, pool *pgxpool.Pool, subject string, want int) {
	t.Helper()

	var count int

	err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM source_observations WHERE provider_id = (SELECT id FROM source_observation_providers WHERE name = 'youtubejs') AND subject_key = $1
	`, subject).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != want {
		t.Fatalf("%s observations = %d, want %d", subject, count, want)
	}
}
