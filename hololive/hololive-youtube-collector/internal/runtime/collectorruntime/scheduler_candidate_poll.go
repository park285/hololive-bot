package collectorruntime

import (
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

// 다른 프로세스의 not_before 앞당김·이른 DEFERRED 전환을 놓치는 시간을 제한한다.
const maxCandidatePollSkip = 10 * time.Second

type candidatePollState struct {
	skipUntil time.Time
	revision  uint64
}

func (s *leaseScheduler) skipCandidatePoll(id collection.JobID, now time.Time) bool {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	return now.Before(s.candidatePolls[id].skipUntil)
}

func (s *leaseScheduler) candidatePollRevision(id collection.JobID) uint64 {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	return s.candidatePolls[id].revision
}

func (s *leaseScheduler) recordCandidatePoll(id collection.JobID, generation int64, revision uint64, started time.Time, page joblease.CandidatePage, limit int, err error) {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	state := s.candidatePolls[id]
	// 조회 중 끝난 작업의 무효화를 오래된 snapshot의 힌트가 덮어쓰지 않는다.
	if s.projection != generation || state.revision != revision {
		return
	}

	state.skipUntil = time.Time{}

	if err == nil && !page.Truncated && len(page.Jobs) < limit {
		state.skipUntil = started.Add(maxCandidatePollSkip)

		if !page.NextDueAt.IsZero() {
			state.skipUntil = minTime(state.skipUntil, page.NextDueAt)
		}
	}

	if s.candidatePolls == nil {
		s.candidatePolls = make(map[collection.JobID]candidatePollState)
	}

	s.candidatePolls[id] = state
}

func (s *leaseScheduler) invalidateCandidatePoll(id collection.JobID) {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	if s.candidatePolls == nil {
		s.candidatePolls = make(map[collection.JobID]candidatePollState)
	}

	state := s.candidatePolls[id]

	state.skipUntil = time.Time{}
	state.revision++

	s.candidatePolls[id] = state
}
