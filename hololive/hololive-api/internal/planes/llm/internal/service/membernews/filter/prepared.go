package filter

import (
	"slices"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type preparedCandidate struct {
	item     datedCandidate
	tokens   map[string]struct{}
	body     func() string
	category model.Category
}

// PreparedCandidates는 한 실행의 후보를 소유합니다. 방별 멤버·alias와 URL 검증은 각 방 처리 때 관측합니다.
// 본문 정규화는 정확 멤버 token으로 해결되지 않은 profile이 있을 때 실행 수명 안에서 한 번만 수행합니다.
type PreparedCandidates struct {
	candidates []preparedCandidate
}

func PrepareCandidates(candidates []model.Candidate, period model.Period, now time.Time) *PreparedCandidates {
	dated := applyPeriodFilter(candidates, period, now)
	prepared := &PreparedCandidates{candidates: make([]preparedCandidate, 0, len(dated))}

	for i := range dated {
		item := dated[i]

		item.candidate = cloneCandidate(item.candidate)

		candidate := item.candidate

		prepared.candidates = append(prepared.candidates, preparedCandidate{
			item:     item,
			tokens:   buildCandidateMemberTokenSet(candidate.Members),
			body:     sync.OnceValue(func() string { return stringutil.NormalizeKey(candidate.Title + " " + candidate.Description) }),
			category: classifyCategory(&candidate),
		})
	}

	return prepared
}

func (p *PreparedCandidates) Filter(roomMembers []string, membersData domain.MemberDataProvider, sourceValidator model.SourceURLValidator) []model.FilteredCandidate {
	profiles := buildMemberProfiles(roomMembers, membersData)
	result := make([]model.FilteredCandidate, 0, len(p.candidates))

	for i := range p.candidates {
		prepared := &p.candidates[i]
		matched := matchPreparedMembers(profiles, prepared.tokens, prepared.body)

		if len(matched) == 0 {
			continue
		}

		tier, sourceURL, ok := resolveSource(&prepared.item.candidate, sourceValidator)
		if !ok {
			continue
		}

		result = append(result, model.FilteredCandidate{
			Candidate:      cloneCandidate(prepared.item.candidate),
			EffectiveDate:  prepared.item.date,
			MatchedMembers: matched,
			MemberText:     formatMemberText(matched),
			Category:       prepared.category,
			SourceTier:     tier,
			SourceURL:      sourceURL,
		})
	}

	slices.SortStableFunc(result, compareFilteredCandidate)

	return result
}

func cloneCandidate(candidate model.Candidate) model.Candidate {
	candidate.Members = slices.Clone(candidate.Members)
	if candidate.PubDate != nil {
		candidate.PubDate = new(*candidate.PubDate)
	}

	if candidate.EventStartDate != nil {
		candidate.EventStartDate = new(*candidate.EventStartDate)
	}

	return candidate
}
