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

package checker

import (
	"slices"
	"time"
)

// MinutesUntilFloor는 start까지 남은 시간을 분 단위로 내림 계산한다.
// 과거이거나 현재이면 0을 반환한다.
func MinutesUntilFloor(start, now time.Time) int {
	secs := start.Sub(now) / time.Second
	if secs <= 0 {
		return 0
	}
	return int(secs / 60)
}

// FormatScheduleChangeMessage는 일정 변경 안내 문구를 반환한다.
// oldTime < newTime: 늦춰짐, oldTime > newTime: 앞당겨짐, 같으면 빈 문자열.
func FormatScheduleChangeMessage(oldTime, newTime time.Time) string {
	if oldTime.Before(newTime) {
		return "일정이 늦춰졌습니다."
	}
	if oldTime.After(newTime) {
		return "일정이 앞당겨졌습니다."
	}
	return ""
}

// IsTargetMinute는 minutesUntil 값이 targetMinutes에 포함되는지 확인한다.
func IsTargetMinute(targetMinutes []int, minutesUntil int) bool {
	return slices.Contains(targetMinutes, minutesUntil)
}
