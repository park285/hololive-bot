package collectorruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/park285/shared-go/pkg/workercontract"
)

type SchedulerState string

const (
	SchedulerNew      SchedulerState = "NEW"
	SchedulerRunning  SchedulerState = "RUNNING"
	SchedulerStopping SchedulerState = "STOPPING"
	SchedulerStopped  SchedulerState = "STOPPED"
)

type EnqueueResult string

const (
	EnqueueAccepted EnqueueResult = "ACCEPTED"
	EnqueueDeduped  EnqueueResult = "DEDUPED"
	EnqueueFull     EnqueueResult = "FULL"
	EnqueueCanceled EnqueueResult = "CANCELED"
	EnqueueInvalid  EnqueueResult = "INVALID"
)

type SchedulerSnapshot struct {
	State                  SchedulerState
	QueueDepth             int
	QueueCapacity          int
	Discovered             int
	Enqueued               int
	Deduped                int
	QueueFull              bool
	DiscoveryTruncated     bool
	Projection             int64
	RotationCursor         int
	CycleStartedAt         time.Time
	LastCycleCompletedAt   time.Time
	LastCycleOperationCode collecterr.OperationCode
	OldestQueueAge         time.Duration
}

type projectionCandidateSource interface {
	CurrentProjectionGeneration(ctx context.Context) (int64, error)
	CandidatesForProjection(
		ctx context.Context,
		generation int64,
		job sourceobservation.JobContract,
		excludedJobKeys []string,
		limit int,
	) (joblease.CandidatePage, error)
}

type leaseScheduler struct {
	repository *joblease.Repository
	candidates projectionCandidateSource
	registry   *Registry
	publisher  *Publisher
	metrics    *Metrics
	owner      string
	logger     *slog.Logger
	config     joblease.Config
	collector  settings.YouTubeCollectorConfig
	gates      map[contract.Provider]chan struct{}

	mu                     sync.Mutex
	state                  SchedulerState
	queued                 map[string]struct{}
	queuedAt               map[string]time.Time
	queue                  chan joblease.JobSpec
	cancel                 context.CancelFunc
	done                   chan struct{}
	fatal                  chan error
	fatalOnce              sync.Once
	wg                     sync.WaitGroup
	readiness              *readinessTracker
	rotationCursor         int
	discovered             int
	enqueued               int
	deduped                int
	queueFull              bool
	discoveryTruncated     bool
	projection             int64
	cycleStartedAt         time.Time
	lastCycleCompletedAt   time.Time
	lastCycleOperationCode collecterr.OperationCode
	workerTracker          *workercontract.ExecutorTracker
	workerTotals           *workercontract.Counters
}

func (s *leaseScheduler) readinessTrackerRef() *readinessTracker {
	if s == nil {
		return nil
	}
	return s.readiness
}

func (s *leaseScheduler) Start(parent context.Context) error {
	if s == nil || s.repository == nil || s.registry == nil {
		return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "start lease scheduler: scheduler is not configured")
	}
	s.mu.Lock()
	if s.lifecycleState() != SchedulerNew {
		s.mu.Unlock()
		return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "start lease scheduler: instance is not NEW")
	}
	runCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	s.state = SchedulerRunning
	s.workerTracker.StartWorkers(s.config.WorkerCount)
	s.wg.Add(s.config.WorkerCount + 1)
	s.mu.Unlock()
	for i := 0; i < s.config.WorkerCount; i++ {
		panicguard.Go(s.logger, "youtube-collector-worker", func() {
			s.worker(runCtx)
		})
	}
	panicguard.Go(s.logger, "youtube-collector-discovery", func() {
		s.discover(runCtx)
	})
	panicguard.Go(s.logger, "youtube-collector-join", func() {
		s.join(done)
	})
	return nil
}

func (s *leaseScheduler) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	cancel, done, wait, err := s.prepareStop()
	if err != nil {
		return err
	}
	if cancel != nil {
		cancel()
	}
	if !wait {
		return nil
	}
	return s.waitDone(ctx, done)
}

func (s *leaseScheduler) prepareStop() (
	cancel context.CancelFunc,
	done chan struct{},
	wait bool,
	err error,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.lifecycleState()
	if state == SchedulerNew {
		s.state = SchedulerStopped
		return nil, nil, false, nil
	}
	if state == SchedulerStopped {
		return nil, nil, false, nil
	}
	if state == SchedulerRunning {
		s.state = SchedulerStopping
		return s.cancel, s.done, true, nil
	}
	if state == SchedulerStopping {
		return nil, s.done, true, nil
	}
	return nil, nil, false, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "stop lease scheduler: state is invalid")
}

func (s *leaseScheduler) waitDone(ctx context.Context, done chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop lease scheduler: %w", ctx.Err())
	}
}

func (s *leaseScheduler) join(done chan struct{}) {
	s.wg.Wait()
	s.workerTracker.StopWorkers(s.config.WorkerCount)
	s.drainQueue()
	s.mu.Lock()
	s.state = SchedulerStopped
	s.mu.Unlock()
	close(done)
}

func (s *leaseScheduler) discover(ctx context.Context) {
	defer s.wg.Done()
	if err := panicguard.RunE(s.logger, "youtube-collector-discovery", func() error {
		ticker := time.NewTicker(s.config.PollCadence)
		defer ticker.Stop()
		s.pollGuarded(ctx)
		for s.waitPoll(ctx, ticker) {
			s.pollGuarded(ctx)
		}
		return nil
	}); err != nil {
		s.reportFatal(collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err))
	}
}

func (s *leaseScheduler) pollGuarded(ctx context.Context) {
	if err := panicguard.RunE(s.logger, "youtube-collector-poll", func() error {
		s.pollOnce(ctx)
		return nil
	}); err != nil {
		s.reportFatal(collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err))
	}
}

func (s *leaseScheduler) pollOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	s.discoverOnce(ctx)
	if ctx.Err() == nil {
		s.refreshFreshness(time.Now().UTC())
	}
}

func (s *leaseScheduler) waitPoll(ctx context.Context, ticker *time.Ticker) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ticker.C:
		return true
	}
}

func (s *leaseScheduler) refreshFreshness(now time.Time) {
	if s.metrics == nil || s.registry == nil {
		return
	}
	for _, runner := range s.registry.Runners() {
		id := runner.Contract().ID()
		s.metrics.ObserveFreshness(id.Provider, string(id.Kind), now)
	}
}

type FatalRuntimeError struct {
	Phase string
	Err   error
}

func (e *FatalRuntimeError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("youtube collector scheduler fatal in %s: %v", e.Phase, e.Err)
}

func (e *FatalRuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (s *leaseScheduler) Fatal() <-chan error {
	if s == nil {
		return nil
	}
	return s.fatal
}

func (s *leaseScheduler) reportFatal(err error) {
	if s == nil || err == nil {
		return
	}
	s.fatalOnce.Do(func() {
		s.emitFatal(collecterr.Normalize(err))
	})
}

func (s *leaseScheduler) emitFatal(err error) {
	s.cancelDiscoveryAndWorkers()
	if s.fatal == nil {
		return
	}
	select {
	case s.fatal <- err:
	default:
	}
}

func (s *leaseScheduler) cancelDiscoveryAndWorkers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifecycleState() == SchedulerRunning {
		s.state = SchedulerStopping
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *leaseScheduler) Snapshot() SchedulerSnapshot {
	if s == nil {
		return SchedulerSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	depth := 0
	oldestQueueAge := time.Duration(0)
	if s.queue != nil {
		depth = len(s.queue)
	}
	now := time.Now()
	for _, queuedAt := range s.queuedAt {
		age := now.Sub(queuedAt)
		if age > oldestQueueAge {
			oldestQueueAge = age
		}
	}
	return SchedulerSnapshot{
		State:                  s.lifecycleState(),
		QueueDepth:             depth,
		QueueCapacity:          s.config.QueueCapacity,
		Discovered:             s.discovered,
		Enqueued:               s.enqueued,
		Deduped:                s.deduped,
		QueueFull:              s.queueFull,
		DiscoveryTruncated:     s.discoveryTruncated,
		Projection:             s.projection,
		RotationCursor:         s.rotationCursor,
		CycleStartedAt:         s.cycleStartedAt,
		LastCycleCompletedAt:   s.lastCycleCompletedAt,
		LastCycleOperationCode: s.lastCycleOperationCode,
		OldestQueueAge:         oldestQueueAge,
	}
}

func (s *leaseScheduler) lifecycleState() SchedulerState {
	if s.state == "" {
		return SchedulerNew
	}
	return s.state
}
