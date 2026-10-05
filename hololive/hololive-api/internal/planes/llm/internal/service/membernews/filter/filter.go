// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package filter

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

var kst = timeutil.KSTZone

type datedCandidate struct {
	candidate model.Candidate
	date      time.Time
}

type memberProfile struct {
	display string
	tokens  []string
}

// FilterCandidates는 기간·멤버·출처 조건을 통과한 후보를 돌려준다. 멤버 데이터 조회 실패는 오류다.
func FilterCandidates(
	ctx context.Context,
	candidates []model.Candidate,
	period model.Period,
	now time.Time,
	roomMembers []string,
	membersData domain.MemberDataProvider,
	sourceValidator model.SourceURLValidator,
) ([]model.FilteredCandidate, error) {
	periodCandidates := applyPeriodFilter(candidates, period, now)

	profiles, err := buildMemberProfiles(ctx, roomMembers, membersData)
	if err != nil {
		return nil, fmt.Errorf("build member profiles: %w", err)
	}

	result := make([]model.FilteredCandidate, 0, len(periodCandidates))
	for i := range periodCandidates {
		filtered, ok := buildFilteredCandidate(&periodCandidates[i], profiles, sourceValidator)
		if ok {
			result = append(result, filtered)
		}
	}

	slices.SortStableFunc(result, compareFilteredCandidate)

	return result, nil
}
