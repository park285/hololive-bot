package membernews

import (
	"time"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/filter"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// FilterCandidates: 기간/멤버/카테고리/출처 검증을 모두 적용합니다. (호환 wrapper)
func FilterCandidates(
	candidates []Candidate,
	period Period,
	now time.Time,
	roomMembers []string,
	membersData domain.MemberDataProvider,
	sourceValidator *SourceValidator,
) []FilteredCandidate {
	// NOTE: typed nil pointer를 interface로 넘기면 nil check가 실패하므로 변환을 명시한다.
	var validator model.SourceURLValidator
	if sourceValidator != nil {
		validator = sourceValidator
	}

	return filter.FilterCandidates(candidates, period, now, roomMembers, membersData, validator)
}
