package collectorruntime

import (
	"context"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func (s *leaseScheduler) discoverOnce(ctx context.Context) {
	if s == nil || ctx.Err() != nil {
		return
	}

	started := time.Now().UTC()
	free, excluded, startCursor := s.discoverySnapshot()
	s.beginCycle(started, free == 0)

	if free == 0 {
		s.finishCycle(started, collecterr.OperationCode(""), false)

		return
	}

	source := s.projectionSource()
	if source == nil {
		s.failCycle(ctx, started, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "discovery cycle: candidate source is missing"))

		return
	}

	dbCtx, cancel := context.WithTimeout(ctx, s.dbTimeout)
	generation, err := source.CurrentProjectionGeneration(dbCtx)

	cancel()

	if err != nil {
		s.failCycle(ctx, started, err)

		return
	}

	s.setProjection(generation)

	runners := s.discoveryRunners()
	if len(runners) == 0 {
		s.finishCycle(started, collecterr.OperationCode(""), true)

		return
	}

	start := startCursor % len(runners)
	outcome := runCapacityAwareCycle(&capacityCycleRequest{
		runnerCount: len(runners),
		start:       start,
		remaining:   free,
		batch:       s.acquisitionBatch,
		excluded:    excluded,
		query:       s.queryRunnerPage(ctx, source, generation, runners),
		enqueue:     func(spec *joblease.JobSpec) EnqueueResult { return s.enqueueDiscovered(ctx, spec) },
		warnFull:    s.warnQueueFullOnce(),
	})
	s.recordCycle(started, generation, &outcome, start, len(runners))

	for _, failure := range outcome.failures {
		s.logRunnerDiscoveryFailure(ctx, failure, runners)
	}
}

func (s *leaseScheduler) enqueueDiscovered(ctx context.Context, spec *joblease.JobSpec) EnqueueResult {
	result := s.enqueue(ctx, spec)
	s.metrics.ObserveEnqueue(result)

	return result
}

func (s *leaseScheduler) warnQueueFullOnce() func() {
	warned := false

	return func() {
		if warned || s.logger == nil {
			return
		}

		warned = true

		s.logger.Warn("YouTube collector local queue is full",
			"queue_capacity", s.queueCapacity,
		)
	}
}

func (s *leaseScheduler) projectionSource() projectionCandidateSource {
	if s.candidates != nil {
		return s.candidates
	}

	return s.executor.repository
}

func (s *leaseScheduler) discoveryRunners() []RegisteredRunner {
	if s.executor.registry == nil {
		return nil
	}

	return s.executor.registry.Runners()
}

func (s *leaseScheduler) discoverySnapshot() (free int, excluded []string, startCursor int) {
	s.queueMu.Lock()

	queued := len(s.queued)

	free = max(s.queueCapacity-queued, 0)
	excluded = make([]string, 0, queued)

	for key := range s.queued {
		excluded = append(excluded, key)
	}

	s.queueMu.Unlock()

	s.cycleMu.Lock()

	startCursor = s.rotationCursor
	s.cycleMu.Unlock()

	return free, excluded, startCursor
}

func (s *leaseScheduler) beginCycle(started time.Time, queueFull bool) {
	s.cycleMu.Lock()

	s.cycleStartedAt = started
	s.discovered = 0
	s.enqueued = 0
	s.deduped = 0
	s.queueFull = queueFull
	s.discoveryTruncated = false
	s.lastCycleOperationCode = ""
	s.cycleMu.Unlock()
}

func (s *leaseScheduler) setProjection(generation int64) {
	s.cycleMu.Lock()

	s.projection = generation
	s.cycleMu.Unlock()
}

func (s *leaseScheduler) recordCycle(
	started time.Time,
	generation int64,
	outcome *capacityCycleResult,
	start, total int,
) {
	s.cycleMu.Lock()

	s.cycleStartedAt = started
	s.projection = generation
	s.discovered = outcome.discovered
	s.enqueued = outcome.enqueued
	s.deduped = outcome.deduped
	s.queueFull = outcome.queueFull
	s.discoveryTruncated = outcome.truncated
	s.lastCycleCompletedAt = time.Now().UTC()

	if outcome.queryErr != nil {
		s.lastCycleOperationCode = collecterr.OperationCandidateLoadFailed
	} else {
		s.lastCycleOperationCode = ""
	}

	if total > 0 {
		s.rotationCursor = nextRotationCursor(start, total, outcome)
	}

	s.cycleMu.Unlock()
}

func (s *leaseScheduler) finishCycle(started time.Time, code collecterr.OperationCode, rotate bool) {
	s.cycleMu.Lock()

	s.cycleStartedAt = started
	s.lastCycleCompletedAt = time.Now().UTC()
	s.lastCycleOperationCode = code

	if rotate {
		total := 0

		if s.executor.registry != nil {
			total = len(s.executor.registry.Runners())
		}

		if total > 0 {
			s.rotationCursor = (s.rotationCursor + 1) % total
		}
	}

	s.cycleMu.Unlock()
}

func (s *leaseScheduler) failCycle(ctx context.Context, started time.Time, err error) {
	s.cycleMu.Lock()

	s.cycleStartedAt = started
	s.lastCycleCompletedAt = time.Now().UTC()
	s.lastCycleOperationCode = collecterr.OperationCandidateLoadFailed
	s.cycleMu.Unlock()
	s.logDiscoveryFailure(ctx, err)
}

func (s *leaseScheduler) logDiscoveryFailure(ctx context.Context, err error) {
	if supersededError(err) {
		return
	}

	spec := joblease.JobSpec{}
	proof := contract.LeaseProof{}
	s.executor.logFailure(ctx, "candidate_load", string(collecterr.CandidateFailed), string(collecterr.ClassOf(err)), collecterr.DiagnosticOf(err).Detail(), &spec, &proof)
}

func (s *leaseScheduler) logRunnerDiscoveryFailure(ctx context.Context, failure runnerQueryFailure, runners []RegisteredRunner) {
	if supersededError(failure.err) {
		return
	}

	id := runners[failure.runner].Contract().ID()
	spec := joblease.JobSpec{Provider: id.Provider, CollectionJobKind: string(id.Kind)}
	proof := contract.LeaseProof{}
	s.executor.logFailure(ctx, "candidate_load", string(collecterr.CandidateFailed), string(collecterr.ClassOf(failure.err)), collecterr.DiagnosticOf(failure.err).Detail(), &spec, &proof)
}
