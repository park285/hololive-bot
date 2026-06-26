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
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func applyPeriodFilter(candidates []model.Candidate, period model.Period, now time.Time) []datedCandidate {
	rangeStart, rangeEnd := periodRange(period, now)

	result := make([]datedCandidate, 0, len(candidates))
	for i := range candidates {
		candidate := &candidates[i]
		candidateDate, ok := resolveCandidateDate(candidate)
		if !ok {
			continue
		}
		candidateDate = candidateDate.In(kst)
		if withinRange(candidateDate, rangeStart, rangeEnd) {
			result = append(result, datedCandidate{candidate: *candidate, date: candidateDate})
		}
	}

	return result
}

func periodRange(period model.Period, now time.Time) (rangeStart, rangeEnd time.Time) {
	nowKST := now.In(kst)
	if model.NormalizePeriod(period) == model.PeriodMonthly {
		rangeStart := time.Date(nowKST.Year(), nowKST.Month(), 1, 0, 0, 0, 0, kst)
		return rangeStart, rangeStart.AddDate(0, 1, 0).Add(-time.Nanosecond)
	}

	return beginningOfDay(nowKST.AddDate(0, 0, -7)), endOfDay(nowKST.AddDate(0, 0, 21))
}

func withinRange(candidateDate, rangeStart, rangeEnd time.Time) bool {
	return !candidateDate.Before(rangeStart) && !candidateDate.After(rangeEnd)
}

func resolveCandidateDate(candidate *model.Candidate) (time.Time, bool) {
	if candidate.Type == domain.MajorEventTypeNews {
		return firstAvailableDate(candidate.PubDate, candidate.EventStartDate)
	}
	return firstAvailableDate(candidate.EventStartDate, candidate.PubDate)
}

func firstAvailableDate(dates ...*time.Time) (time.Time, bool) {
	for _, date := range dates {
		if date != nil {
			return *date, true
		}
	}
	return time.Time{}, false
}

func beginningOfDay(t time.Time) time.Time {
	local := t.In(kst)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, kst)
}

func endOfDay(t time.Time) time.Time {
	local := t.In(kst)
	return time.Date(local.Year(), local.Month(), local.Day(), 23, 59, 59, int(time.Second-time.Nanosecond), kst)
}
