package collectorruntime

import (
	"context"
	"slices"

	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

type capacityCycleRequest struct {
	runnerIDs []string
	start     int
	remaining int
	batch     int
	excluded  []string
	query     func(runnerID string, excluded []string, limit int) (joblease.CandidatePage, error)
	enqueue   func(*joblease.JobSpec) EnqueueResult
	warnFull  func()
}

type capacityCycleResult struct {
	discovered   int
	enqueued     int
	deduped      int
	truncated    bool
	queueFull    bool
	canceled     bool
	stoppedEarly bool
	queried      int
	queryErr     error
	limits       []int
}

func runCapacityAwareCycle(req *capacityCycleRequest) capacityCycleResult {
	if req == nil {
		return capacityCycleResult{}
	}
	state := capacityCycleState{
		remaining: req.remaining,
		excluded:  slices.Clone(req.excluded),
	}
	total := len(req.runnerIDs)
	if total == 0 || state.remaining <= 0 {
		state.result.queueFull = state.remaining <= 0
		return state.result
	}
	for index := range total {
		if state.runStep(req, index, total) {
			return state.result
		}
	}
	return state.result
}

type capacityCycleState struct {
	remaining int
	excluded  []string
	result    capacityCycleResult
}

func (s *capacityCycleState) runStep(req *capacityCycleRequest, index, total int) bool {
	if s.remaining == 0 {
		s.result.queueFull = true
		s.result.stoppedEarly = true
		return true
	}
	page, err := s.queryPage(req, index, total)
	if err != nil {
		s.result.queryErr = err
		return true
	}
	stop, applied := applyCandidatePage(page, s.remaining, s.excluded, req.enqueue, req.warnFull)
	s.mergePage(applied)
	s.result.stoppedEarly = stop && index+1 < total
	return stop
}

func (s *capacityCycleState) queryPage(req *capacityCycleRequest, index, total int) (joblease.CandidatePage, error) {
	limit := discoveryLimit(s.remaining, total-index, req.batch)
	s.result.limits = append(s.result.limits, limit)
	s.result.queried++
	page, err := req.query(req.runnerIDs[(req.start+index)%total], s.excluded, limit)
	if err != nil {
		return joblease.CandidatePage{}, err
	}
	s.result.discovered += len(page.Jobs)
	s.result.truncated = s.result.truncated || page.Truncated
	return page, nil
}

func (s *capacityCycleState) mergePage(applied pageApplyResult) {
	s.remaining = applied.remaining
	s.excluded = applied.excluded
	s.result.enqueued += applied.enqueued
	s.result.deduped += applied.deduped
	s.result.queueFull = s.result.queueFull || applied.queueFull
	s.result.canceled = s.result.canceled || applied.canceled
}

func nextRotationCursor(start, total int, outcome *capacityCycleResult) int {
	if outcome == nil {
		return start
	}
	if total <= 0 || outcome.queryErr != nil || outcome.canceled {
		return start
	}
	if outcome.stoppedEarly && outcome.queried > 0 {
		return (start + outcome.queried) % total
	}
	return (start + 1) % total
}

type pageApplyResult struct {
	remaining int
	excluded  []string
	enqueued  int
	deduped   int
	queueFull bool
	canceled  bool
}

func applyCandidatePage(
	page joblease.CandidatePage,
	remaining int,
	excluded []string,
	enqueue func(*joblease.JobSpec) EnqueueResult,
	warnFull func(),
) (bool, pageApplyResult) {
	applied := pageApplyResult{remaining: remaining, excluded: excluded}
	for i := range page.Jobs {
		if applyCandidate(&applied, &page.Jobs[i], enqueue, warnFull) {
			return true, applied
		}
		if applied.remaining == 0 {
			applied.queueFull = true
			return true, applied
		}
	}
	return false, applied
}

func applyCandidate(
	applied *pageApplyResult,
	spec *joblease.JobSpec,
	enqueue func(*joblease.JobSpec) EnqueueResult,
	warnFull func(),
) bool {
	result := enqueue(spec)
	if result == EnqueueAccepted {
		applied.excluded = addExcludedKey(applied.excluded, spec.JobKey)
		applied.remaining--
		applied.enqueued++
		return false
	}
	if result == EnqueueDeduped {
		applied.excluded = addExcludedKey(applied.excluded, spec.JobKey)
		applied.deduped++
		return false
	}
	if result == EnqueueFull {
		applied.queueFull = true
		callIfPresent(warnFull)
		return true
	}
	if result == EnqueueCanceled || result == EnqueueInvalid {
		applied.canceled = true
		return true
	}
	return false
}

func callIfPresent(callback func()) {
	if callback != nil {
		callback()
	}
}

func discoveryLimit(remaining, remainingRunners, acquisitionBatch int) int {
	if remaining <= 0 || remainingRunners <= 0 || acquisitionBatch <= 0 {
		return 0
	}
	fairShare := max((remaining+remainingRunners-1)/remainingRunners, 1)
	limit := min(remaining, min(acquisitionBatch, fairShare))
	return limit
}

func addExcludedKey(excluded []string, key string) []string {
	if key == "" || slices.Contains(excluded, key) {
		return excluded
	}
	excluded = append(excluded, key)
	slices.Sort(excluded)
	return excluded
}

func runnerIDs(runners []RegisteredRunner) []string {
	ids := make([]string, len(runners))
	for i, runner := range runners {
		ids[i] = runner.Contract().ID().String()
	}
	return ids
}

func (s *leaseScheduler) queryRunnerPage(
	ctx context.Context,
	source projectionCandidateSource,
	generation int64,
	runners []RegisteredRunner,
) func(runnerID string, excluded []string, limit int) (joblease.CandidatePage, error) {
	byID := make(map[string]sourceobservation.JobContract, len(runners))
	for _, runner := range runners {
		byID[runner.Contract().ID().String()] = runner.Contract()
	}
	return func(runnerID string, excluded []string, limit int) (joblease.CandidatePage, error) {
		job, ok := byID[runnerID]
		if !ok {
			return joblease.CandidatePage{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "discovery cycle: runner identity is missing")
		}
		dbCtx, cancel := context.WithTimeout(ctx, s.collector.DBTimeout)
		defer cancel()
		return source.CandidatesForProjection(dbCtx, generation, job, excluded, limit)
	}
}
