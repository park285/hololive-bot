package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func TestSCH001StartTwiceRejected(t *testing.T) {
	t.Parallel()

	scheduler := newLifecycleScheduler(t)
	if err := scheduler.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := scheduler.Start(t.Context()); err == nil {
		t.Fatal("second Start was accepted")
	}

	if err := scheduler.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSCH002StopBeforeStartAndStopTwice(t *testing.T) {
	t.Parallel()

	scheduler := newLifecycleScheduler(t)
	if err := scheduler.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if scheduler.Snapshot().State != SchedulerStopped {
		t.Fatalf("state = %s, want STOPPED", scheduler.Snapshot().State)
	}

	if err := scheduler.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	if scheduler.Snapshot().State != SchedulerStopped {
		t.Fatalf("second Stop state = %s", scheduler.Snapshot().State)
	}

	if err := scheduler.Start(t.Context()); err == nil {
		t.Fatal("STOPPED scheduler was reused")
	}
}

func TestSCH003QueueFullRollsBackMarkAndIncrementsMetric(t *testing.T) {
	t.Parallel()

	registerer := prometheus.NewPedanticRegistry()
	scheduler := newLifecycleScheduler(t)

	scheduler.metrics = NewMetrics(registerer)
	scheduler.config.QueueCapacity = 1
	scheduler.queue = make(chan joblease.JobSpec, 1)

	scheduler.queue <- joblease.JobSpec{JobKey: "filler"}

	spec := joblease.JobSpec{JobKey: "next"}
	if result := scheduler.enqueueDiscovered(t.Context(), &spec); result != EnqueueFull {
		t.Fatalf("enqueue = %s, want FULL", result)
	}

	if _, ok := scheduler.queued["next"]; ok {
		t.Fatal("FULL enqueue left a queued mark")
	}

	if got := enqueueMetric(t, registerer, EnqueueFull); got != 1 {
		t.Fatalf("full metric = %v, want 1", got)
	}
}

func TestSCH004CanceledBeforeEnqueueAcceptsZero(t *testing.T) {
	t.Parallel()

	scheduler := newLifecycleScheduler(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	spec := joblease.JobSpec{JobKey: "late"}
	if result := scheduler.enqueue(ctx, &spec); result != EnqueueCanceled {
		t.Fatalf("enqueue = %s, want CANCELED", result)
	}

	if len(scheduler.queued) != 0 {
		t.Fatalf("queued = %#v", scheduler.queued)
	}
}

func TestSCH005CancelAfterDequeueDoesNotRun(t *testing.T) {
	t.Parallel()

	scheduler := newLifecycleScheduler(t)
	spec := joblease.JobSpec{JobKey: "ready"}

	scheduler.queued[spec.JobKey] = struct{}{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, ok := scheduler.acceptDequeued(ctx, &spec); ok {
		t.Fatal("canceled dequeue was accepted for run")
	}

	if _, marked := scheduler.queued[spec.JobKey]; marked {
		t.Fatal("canceled dequeue did not unmark")
	}

	scheduler.queued[spec.JobKey] = struct{}{}
	scheduler.queue <- spec

	got, ok := scheduler.nextSpec(ctx)
	if ok {
		t.Fatalf("canceled nextSpec ran %#v", got)
	}
}

func TestSCH006OneCycleProjectionQueryIsOne(t *testing.T) {
	t.Parallel()

	stub := newEmptyCandidateStub(t)
	scheduler := newLifecycleScheduler(t)

	scheduler.candidates = stub
	scheduler.discoverOnce(t.Context())

	if stub.generationCalls != 1 {
		t.Fatalf("projection queries = %d, want 1", stub.generationCalls)
	}
}

func TestSCH007AcceptedAndQueryLimitStayWithinCapacity(t *testing.T) {
	t.Parallel()

	const capacity = 3

	stub := newEmptyCandidateStub(t)
	community := mustSchedulerJob(t, contract.ProviderYouTubeJS, "community_collect")

	stub.pages[community.ID().String()] = joblease.CandidatePage{Jobs: dueSpecs("community", 20)}

	scheduler := newLifecycleScheduler(t)

	scheduler.candidates = stub
	scheduler.config.QueueCapacity = capacity
	scheduler.config.AcquisitionBatch = 10
	scheduler.queue = make(chan joblease.JobSpec, capacity)
	scheduler.discoverOnce(t.Context())

	if scheduler.Snapshot().Enqueued > capacity {
		t.Fatalf("accepted = %d, want <= %d", scheduler.Snapshot().Enqueued, capacity)
	}

	for _, limit := range stub.limits {
		if limit > capacity {
			t.Fatalf("query limit %d exceeds capacity %d", limit, capacity)
		}
	}

	if len(scheduler.queue) > capacity {
		t.Fatalf("queue depth = %d", len(scheduler.queue))
	}
}

func TestSCH008GlobalNotDueDoesNotEnqueue(t *testing.T) {
	t.Parallel()

	stub := newEmptyCandidateStub(t)
	scheduler := newLifecycleScheduler(t)

	scheduler.candidates = stub
	scheduler.discoverOnce(t.Context())

	if scheduler.Snapshot().Enqueued != 0 || scheduler.Snapshot().Discovered != 0 {
		t.Fatalf("snapshot = %+v", scheduler.Snapshot())
	}

	if len(scheduler.queue) != 0 {
		t.Fatal("not-due GLOBAL path enqueued work")
	}
}

func TestSCH009QueuedKeyExclusionKeepsLaterDueKeys(t *testing.T) {
	t.Parallel()

	stub := newEmptyCandidateStub(t)
	community := mustSchedulerJob(t, contract.ProviderYouTubeJS, "community_collect")
	front := joblease.JobSpec{JobKey: "collector:youtubejs:community_collect:channel:a"}
	later := joblease.JobSpec{JobKey: "collector:youtubejs:community_collect:channel:b"}

	stub.pages[community.ID().String()] = joblease.CandidatePage{Jobs: []joblease.JobSpec{front, later}}

	scheduler := newLifecycleScheduler(t)

	scheduler.candidates = stub
	scheduler.queued[front.JobKey] = struct{}{}
	setRotationTo(scheduler, community.ID())
	scheduler.discoverOnce(t.Context())

	if !slices.ContainsFunc(stub.excluded, func(keys []string) bool {
		return slices.Contains(keys, front.JobKey)
	}) {
		t.Fatal("queued key was not passed as exclusion")
	}

	select {
	case spec := <-scheduler.queue:
		if spec.JobKey != later.JobKey {
			t.Fatalf("enqueued %s, want later due key", spec.JobKey)
		}
	default:
		t.Fatal("later due key was hidden by queued exclusion")
	}
}

func TestSCH010QueryErrorFailsWholeCycle(t *testing.T) {
	t.Parallel()

	stub := newEmptyCandidateStub(t)

	stub.err = errors.New("candidate query failed")
	stub.errAfter = 1

	community := mustSchedulerJob(t, contract.ProviderYouTubeJS, "community_collect")

	stub.pages[community.ID().String()] = joblease.CandidatePage{Jobs: dueSpecs("community", 2)}

	scheduler := newLifecycleScheduler(t)

	scheduler.candidates = stub

	cursor := scheduler.rotationCursor
	scheduler.discoverOnce(t.Context())

	if stub.queries != 2 {
		t.Fatalf("queries = %d, want stop after first error (2)", stub.queries)
	}

	if scheduler.rotationCursor != cursor {
		t.Fatal("query error cycle advanced rotation")
	}

	if scheduler.Snapshot().LastCycleOperationCode != collecterr.OperationCandidateLoadFailed {
		t.Fatalf("operation = %s", scheduler.Snapshot().LastCycleOperationCode)
	}
}

func TestSCH011PanicReportsFatalAndStopsAdmission(t *testing.T) {
	t.Parallel()

	stub := &panickingCandidateSource{}
	scheduler := newLifecycleScheduler(t)

	scheduler.candidates = stub

	ctx, cancel := context.WithCancel(t.Context())

	scheduler.cancel = cancel
	scheduler.state = SchedulerRunning
	scheduler.pollGuarded(ctx)

	select {
	case err := <-scheduler.Fatal():
		if err == nil {
			t.Fatal("fatal error was nil")
		}
	case <-time.After(time.Second):
		t.Fatal("panic did not report fatal")
	}

	calls := stub.calls

	scheduler.discoverOnce(ctx)

	if stub.calls != calls {
		t.Fatal("fatal scheduler continued admission")
	}

	spec := joblease.JobSpec{JobKey: "late"}
	if result := scheduler.enqueue(ctx, &spec); result != EnqueueCanceled {
		t.Fatalf("late enqueue = %s", result)
	}
}

func TestSCH012StopDrainsQueueAndQueuedSet(t *testing.T) {
	scheduler := newLifecycleScheduler(t)
	if err := scheduler.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		spec := joblease.JobSpec{JobKey: fmt.Sprintf("drain-%d", i)}

		_ = scheduler.enqueue(t.Context(), &spec)
	}

	if err := scheduler.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	snap := scheduler.Snapshot()
	if snap.State != SchedulerStopped || snap.QueueDepth != 0 {
		t.Fatalf("snapshot = %+v", snap)
	}

	scheduler.mu.Lock()

	queued := len(scheduler.queued)
	scheduler.mu.Unlock()

	if queued != 0 {
		t.Fatalf("queued set = %d", queued)
	}
}

func TestSCH013SnapshotIsConsistentUnderOneLock(t *testing.T) {
	t.Parallel()

	scheduler := newLifecycleScheduler(t)

	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range 1000 {
			scheduler.mu.Lock()

			scheduler.discovered = i
			scheduler.enqueued = i
			scheduler.mu.Unlock()
		}
	})
	wg.Go(func() {
		for range 1000 {
			snap := scheduler.Snapshot()
			if snap.Discovered != snap.Enqueued {
				t.Errorf("torn snapshot discovered=%d enqueued=%d", snap.Discovered, snap.Enqueued)

				return
			}
		}
	})

	wg.Wait()

	snap := scheduler.Snapshot()

	snap.QueueDepth = 99

	if scheduler.Snapshot().QueueDepth == snap.QueueDepth {
		t.Fatal("snapshot mutation leaked")
	}
}

func TestSchedulerFairnessOpportunityBounds(t *testing.T) {
	t.Parallel()

	for _, runnerCount := range []int{1, 2, 3, 8, 20} {
		for _, capacity := range []int{1, 2, runnerCount, runnerCount * 2} {
			for start := range runnerCount {
				seed := fairnessSeed{runnerCount: runnerCount, capacity: capacity, startCursor: start}
				if err := checkFairness(seed); err != nil {
					t.Fatalf("fairness seed=%+v: %v", seed, err)
				}
			}
		}
	}
}

type fairnessSeed struct {
	runnerCount int
	capacity    int
	startCursor int
}

func checkFairness(seed fairnessSeed) error {
	ids := fairnessRunnerIDs(seed.runnerCount)
	bound := fairnessCycleBound(seed)
	seen := make(map[string]int, seed.runnerCount)
	cursor := seed.startCursor

	for cycle := 1; cycle <= bound; cycle++ {
		outcome := runFairnessCycle(seed, ids, cursor, cycle, seen)
		if outcome.queryErr != nil {
			return fmt.Errorf("query error is not a fairness cycle: %w", outcome.queryErr)
		}

		if len(seen) == seed.runnerCount {
			return nil
		}

		cursor = nextRotationCursor(cursor%seed.runnerCount, seed.runnerCount, &outcome)
	}

	return fmt.Errorf("runners without opportunity=%d/%d after %d cycles", len(seen), seed.runnerCount, bound)
}

func fairnessRunnerIDs(count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = fmt.Sprintf("runner-%d", i)
	}

	return ids
}

func fairnessCycleBound(seed fairnessSeed) int {
	if seed.capacity >= seed.runnerCount {
		return 1
	}

	return (seed.runnerCount+seed.capacity-1)/seed.capacity + 1
}

func runFairnessCycle(seed fairnessSeed, ids []string, cursor, cycle int, seen map[string]int) capacityCycleResult {
	accepted := 0

	return runCapacityAwareCycle(&capacityCycleRequest{
		runnerIDs: ids,
		start:     cursor % seed.runnerCount,
		remaining: seed.capacity,
		batch:     seed.capacity,
		query:     fairnessCandidatePage,
		enqueue: func(spec *joblease.JobSpec) EnqueueResult {
			if accepted >= seed.capacity {
				return EnqueueFull
			}

			accepted++

			runnerID := spec.JobKey

			if idx := indexByte(spec.JobKey, ':'); idx >= 0 {
				runnerID = spec.JobKey[:idx]
			}

			if _, ok := seen[runnerID]; !ok {
				seen[runnerID] = cycle
			}

			return EnqueueAccepted
		},
	})
}

func fairnessCandidatePage(runnerID string, excluded []string, limit int) (joblease.CandidatePage, error) {
	jobs := make([]joblease.JobSpec, 0, limit)

	for n := range 32 {
		key := fmt.Sprintf("%s:%d", runnerID, n)
		if !slices.Contains(excluded, key) {
			jobs = append(jobs, joblease.JobSpec{JobKey: key})
		}
	}

	truncated := len(jobs) > limit
	if truncated {
		jobs = jobs[:limit]
	}

	return joblease.CandidatePage{Jobs: jobs, Truncated: truncated}, nil
}

func indexByte(value string, b byte) int {
	for i := range len(value) {
		if value[i] == b {
			return i
		}
	}

	return -1
}

type stubCandidateSource struct {
	mu              sync.Mutex
	generation      int64
	generationCalls int
	pages           map[string]joblease.CandidatePage
	err             error
	errAfter        int
	queries         int
	limits          []int
	excluded        [][]string
}

func (s *stubCandidateSource) CurrentProjectionGeneration(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.generationCalls++
	if s.generation == 0 {
		return 1, nil
	}

	return s.generation, nil
}

func (s *stubCandidateSource) CandidatesForProjection(
	_ context.Context,
	_ int64,
	job sourceobservation.JobContract,
	excludedJobKeys []string,
	limit int,
) (joblease.CandidatePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.queries++

	s.limits = append(s.limits, limit)
	s.excluded = append(s.excluded, slices.Clone(excludedJobKeys))

	if s.err != nil && (s.errAfter == 0 || s.queries > s.errAfter) {
		return joblease.CandidatePage{}, s.err
	}

	page := s.pages[job.ID().String()]
	jobs := make([]joblease.JobSpec, 0, len(page.Jobs))

	for _, spec := range page.Jobs {
		if slices.Contains(excludedJobKeys, spec.JobKey) {
			continue
		}

		jobs = append(jobs, spec)
	}

	truncated := len(jobs) > limit || page.Truncated
	if len(jobs) > limit {
		jobs = jobs[:limit]
		truncated = true
	}

	return joblease.CandidatePage{Jobs: jobs, Truncated: truncated}, nil
}

type panickingCandidateSource struct {
	calls int
}

func (s *panickingCandidateSource) CurrentProjectionGeneration(context.Context) (int64, error) {
	s.calls++
	return 1, nil
}

func (s *panickingCandidateSource) CandidatesForProjection(
	context.Context, int64, sourceobservation.JobContract, []string, int,
) (joblease.CandidatePage, error) {
	s.calls++

	panic("candidate source invariant")
}

func newEmptyCandidateStub(t *testing.T) *stubCandidateSource {
	t.Helper()

	pages := make(map[string]joblease.CandidatePage)

	for _, job := range sourceobservation.InitialJobContracts().IDs() {
		pages[job.String()] = joblease.CandidatePage{}
	}

	return &stubCandidateSource{pages: pages}
}

func newLifecycleScheduler(t *testing.T) *leaseScheduler {
	t.Helper()

	config := runtimeLeaseConfig()

	registry, err := NewRegistry(completeStubRunners()...)
	if err != nil {
		t.Fatal(err)
	}

	stub := newEmptyCandidateStub(t)

	return &leaseScheduler{
		repository: new(joblease.Repository),
		candidates: stub,
		registry:   registry,
		metrics:    NewMetrics(prometheus.NewPedanticRegistry()),
		logger:     slog.New(slog.DiscardHandler),
		config:     config,
		collector:  settings.DefaultYouTubeCollectorConfig(),
		state:      SchedulerNew,
		queued:     make(map[string]struct{}),
		queue:      make(chan joblease.JobSpec, config.QueueCapacity),
		fatal:      make(chan error, 1),
		readiness:  &readinessTracker{},
	}
}

func dueSpecs(prefix string, count int) []joblease.JobSpec {
	specs := make([]joblease.JobSpec, count)
	for i := range count {
		specs[i] = joblease.JobSpec{JobKey: fmt.Sprintf("%s-%d", prefix, i)}
	}

	return specs
}

func mustSchedulerJob(t *testing.T, provider contract.Provider, kind string) sourceobservation.JobContract {
	t.Helper()

	job, ok := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: provider, Kind: sourceobservation.JobKind(kind),
	})
	if !ok {
		t.Fatalf("missing job %s/%s", provider, kind)
	}

	return job
}

func setRotationTo(scheduler *leaseScheduler, id sourceobservation.JobID) {
	runners := scheduler.registry.Runners()
	for i, runner := range runners {
		if runner.Contract().ID() == id {
			scheduler.rotationCursor = i
			return
		}
	}
}

func enqueueMetric(t *testing.T, gatherer prometheus.Gatherer, result EnqueueResult) float64 {
	t.Helper()

	families, err := gatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}

	for _, family := range families {
		if family.GetName() != "youtube_collection_enqueue_total" {
			continue
		}

		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "result" && label.GetValue() == string(result) {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}

	return 0
}
