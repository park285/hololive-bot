package collectorruntime

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

type leaseScheduler struct {
	repository *joblease.Repository
	registry   *Registry
	publisher  *Publisher
	metrics    *Metrics
	owner      string
	logger     *slog.Logger
	config     joblease.Config
	collector  settings.YouTubeCollectorConfig
	gates      map[contract.Provider]chan struct{}

	mu      sync.Mutex
	queued  map[string]struct{}
	queue   chan joblease.JobSpec
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	lastDue int
}

func (s *leaseScheduler) Start(ctx context.Context) {
	if s == nil || s.repository == nil || s.registry == nil {
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()
	for i := 0; i < s.config.WorkerCount; i++ {
		s.wg.Add(1)
		go s.worker(runCtx)
	}
	s.wg.Add(1)
	go s.discover(runCtx)
}

func (s *leaseScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		s.wg.Wait()
	}
}

func (s *leaseScheduler) discover(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.config.PollCadence)
	defer ticker.Stop()
	s.pollOnce(ctx)
	for s.waitPoll(ctx, ticker) {
		s.pollOnce(ctx)
	}
}

func (s *leaseScheduler) pollOnce(ctx context.Context) {
	s.syncCandidates(ctx)
	s.refreshFreshness(time.Now().UTC())
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
		s.metrics.ObserveFreshness(runner.Provider(), runner.JobKind(), now)
	}
}

func (s *leaseScheduler) syncCandidates(ctx context.Context) {
	globals, subjects := s.loadCandidates(ctx)
	if !s.enqueueAll(ctx, globals) {
		return
	}
	s.enqueueAll(ctx, subjects)
}

func (s *leaseScheduler) DueJobs() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastDue
}

func (s *leaseScheduler) loadCandidates(ctx context.Context) (globals, subjects []joblease.JobSpec) {
	due := 0
	for _, runner := range s.registry.Runners() {
		candidates, err := s.repository.Candidates(ctx, runner.Provider(), runner.JobKind(), s.config.AcquisitionBatch)
		if err != nil {
			s.logCandidateLoad(runner, err)
			continue
		}
		due += len(candidates)
		globals, subjects = splitCandidates(candidates, globals, subjects)
	}
	s.mu.Lock()
	s.lastDue = due
	s.mu.Unlock()
	return globals, subjects
}

func (s *leaseScheduler) logCandidateLoad(runner JobRunner, err error) {
	if errors.Is(err, joblease.ErrProjectionStale) || errors.Is(err, joblease.ErrTargetDisabled) {
		return
	}
	s.logFailure("candidate_load", collecterr.CandidateFailed, joblease.JobSpec{
		Provider: runner.Provider(), CollectionJobKind: runner.JobKind(),
	}, contract.LeaseProof{})
}

func splitCandidates(candidates, globals, subjects []joblease.JobSpec) ([]joblease.JobSpec, []joblease.JobSpec) {
	for _, candidate := range candidates {
		if candidate.Class == "GLOBAL" {
			globals = append(globals, candidate)
			continue
		}
		subjects = append(subjects, candidate)
	}
	return globals, subjects
}

func (s *leaseScheduler) enqueueAll(ctx context.Context, candidates []joblease.JobSpec) bool {
	for _, candidate := range candidates {
		if !s.enqueue(ctx, candidate) {
			return false
		}
	}
	return true
}

func (s *leaseScheduler) enqueue(ctx context.Context, candidate joblease.JobSpec) bool {
	if !s.markQueued(candidate.JobKey) {
		return true
	}
	select {
	case s.queue <- candidate:
		return true
	case <-ctx.Done():
		s.unmarkQueued(candidate.JobKey)
		return false
	default:
		s.unmarkQueued(candidate.JobKey)
		return true
	}
}

func (s *leaseScheduler) worker(ctx context.Context) {
	defer s.wg.Done()
	for {
		spec, ok := s.nextSpec(ctx)
		if !ok {
			return
		}
		s.runSpec(ctx, spec)
		s.unmarkQueued(spec.JobKey)
	}
}

func (s *leaseScheduler) nextSpec(ctx context.Context) (joblease.JobSpec, bool) {
	select {
	case <-ctx.Done():
		return joblease.JobSpec{}, false
	case spec := <-s.queue:
		return spec, true
	}
}

func (s *leaseScheduler) markQueued(jobKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.queued[jobKey]; exists {
		return false
	}
	s.queued[jobKey] = struct{}{}
	return true
}

func (s *leaseScheduler) unmarkQueued(jobKey string) {
	s.mu.Lock()
	delete(s.queued, jobKey)
	s.mu.Unlock()
}
