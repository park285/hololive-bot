package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

// capacityCycleRequest는 runnerCount개의 런너를 start부터 순환 조회합니다.
// 조회와 실패 결과의 runner 값은 호출자가 넘긴 런너 목록의 위치입니다.
type capacityCycleRequest struct {
	runnerCount int
	start       int
	remaining   int
	batch       int
	excluded    []string
	query       func(runner int, excluded []string, limit int) (joblease.CandidatePage, error)
	enqueue     func(*joblease.JobSpec) EnqueueResult
	warnFull    func()
}

type capacityCycleResult struct {
	discovered    int
	enqueued      int
	deduped       int
	truncated     bool
	queueFull     bool
	canceled      bool
	stoppedEarly  bool
	queried       int
	queryErr      error
	globalFailure bool
	failures      []runnerQueryFailure
	limits        []int
}

type runnerQueryFailure struct {
	runner int
	err    error
}

func runCapacityAwareCycle(req *capacityCycleRequest) capacityCycleResult {
	if req == nil {
		return capacityCycleResult{}
	}

	state := capacityCycleState{
		remaining: req.remaining,
		excluded:  slices.Clone(req.excluded),
	}
	total := req.runnerCount

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
		s.result.queryErr = errors.Join(s.result.queryErr, err)
		s.result.failures = append(s.result.failures, runnerQueryFailure{runner: (req.start + index) % total, err: err})
		// 계약 오류는 해당 런너에서만 닫고, 전역 장애와 취소는 뒤 조회를 중단합니다.
		s.result.globalFailure = !errors.Is(err, joblease.ErrCandidateContract) ||
			errors.Is(err, collection.ErrProjectionStale) ||
			errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)

		return s.result.globalFailure
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

	page, err := req.query((req.start+index)%total, s.excluded, limit)
	if err != nil {
		return joblease.CandidatePage{}, fmt.Errorf("query: %w", err)
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

	if total <= 0 || outcome.globalFailure || outcome.canceled {
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

// addExcludedKey는 중복 없이 키를 추가합니다. 정렬·정규화는 저장소의 normalizeExcludedJobKeys가 소유합니다.
func addExcludedKey(excluded []string, key string) []string {
	if key == "" || slices.Contains(excluded, key) {
		return excluded
	}

	return append(excluded, key)
}

func (s *leaseScheduler) queryRunnerPage(
	ctx context.Context,
	source projectionCandidateSource,
	generation int64,
	runners []RegisteredRunner,
) func(runner int, excluded []string, limit int) (joblease.CandidatePage, error) {
	return func(runner int, excluded []string, limit int) (joblease.CandidatePage, error) {
		if err := ctx.Err(); err != nil {
			return joblease.CandidatePage{}, fmt.Errorf("query runner page: %w", err)
		}

		dbCtx, cancel := context.WithTimeout(ctx, s.dbTimeout)

		defer cancel()

		page, err := source.CandidatesForProjection(dbCtx, generation, runners[runner].Contract(), excluded, limit)

		if ctxErr := ctx.Err(); ctxErr != nil {
			return joblease.CandidatePage{}, fmt.Errorf("query runner page: %w", errors.Join(err, ctxErr))
		}

		if err != nil {
			return page, fmt.Errorf("query runner page: %w", err)
		}

		return page, nil
	}
}
