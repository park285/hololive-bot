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
	"sort"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	kst = model.KST
)

type datedCandidate struct {
	candidate model.Candidate
	date      time.Time
}

type memberProfile struct {
	display string
	tokens  []string
}

func FilterCandidates(
	candidates []model.Candidate,
	period model.Period,
	now time.Time,
	roomMembers []string,
	membersData domain.MemberDataProvider,
	sourceValidator model.SourceURLValidator,
) []model.FilteredCandidate {
	periodCandidates := applyPeriodFilter(candidates, period, now)
	profiles := buildMemberProfiles(roomMembers, membersData)

	result := make([]model.FilteredCandidate, 0, len(periodCandidates))
	for i := range periodCandidates {
		filtered, ok := buildFilteredCandidate(&periodCandidates[i], profiles, sourceValidator)
		if ok {
			result = append(result, filtered)
		}
	}

	sort.SliceStable(result, func(i, j int) bool {
		return lessFilteredCandidate(&result[i], &result[j])
	})

	return result
}
