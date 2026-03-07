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

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// ComputeNextCheckAt: 방송 시작까지 남은 시간 기반으로 다음 체크 시각을 계산한다.
// nearestStart가 nil이면 예정 없음 로직을 적용한다.
func ComputeNextCheckAt(nearestStart *time.Time, lastNotifiedAt *time.Time) time.Time {
	now := time.Now()

	if nearestStart == nil {
		// 예정 없음: 최근 알림 발송 채널은 고빈도 폴링 유지
		if lastNotifiedAt != nil {
			elapsed := now.Sub(*lastNotifiedAt)
			if elapsed <= constants.RecentlyNotifiedWindow {
				return now.Add(constants.Tier2Interval)
			}
		}
		return now.Add(constants.NoUpcomingInterval)
	}

	timeToStart := nearestStart.Sub(now)

	// 이미 지남 또는 현재 -> Tier1
	if timeToStart <= 0 {
		return now.Add(constants.Tier1Interval)
	}

	switch {
	case timeToStart <= constants.Tier1Window:
		return now.Add(constants.Tier1Interval)
	case timeToStart <= constants.Tier2Window:
		return now.Add(constants.Tier2Interval)
	case timeToStart <= constants.Tier3Window:
		return now.Add(constants.Tier3Interval)
	default:
		return now.Add(constants.Tier4Interval)
	}
}
