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

package tier

import "time"

// ComputeNextCheckAt은 now(평가 시각) 기준 다음 점검 시각을 정한다. 가장 가까운 예정 시작(nearestStart)이 nil이면 예정 없음 로직을 적용한다.
func ComputeNextCheckAt(now time.Time, nearestStart, lastNotifiedAt *time.Time) time.Time {
	if nearestStart == nil {
		return nextCheckAtWithoutUpcoming(now, lastNotifiedAt)
	}

	timeToStart := nearestStart.Sub(now)

	return nextCheckAtForTimeToStart(now, timeToStart)
}

func nextCheckAtWithoutUpcoming(now time.Time, lastNotifiedAt *time.Time) time.Time {
	if isRecentlyNotified(now, lastNotifiedAt) {
		return now.Add(min(tier2Interval, noUpcomingInterval))
	}

	return now.Add(noUpcomingInterval)
}

func isRecentlyNotified(now time.Time, lastNotifiedAt *time.Time) bool {
	return lastNotifiedAt != nil && now.Sub(*lastNotifiedAt) <= recentlyNotifiedWindow
}

func nextCheckAtForTimeToStart(now time.Time, timeToStart time.Duration) time.Time {
	switch {
	case timeToStart <= tier1Window:
		return now.Add(tier1Interval)
	case timeToStart <= tier2Window:
		return now.Add(tier2Interval)
	case timeToStart <= tier3Window:
		return now.Add(tier3Interval)
	default:
		return now.Add(tier4Interval)
	}
}
