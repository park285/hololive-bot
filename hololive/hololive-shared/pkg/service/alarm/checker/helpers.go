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

var defaultTargetMinutes = []int{5, 3, 1}

func cloneDefaultTargetMinutes() []int {
	return append([]int(nil), defaultTargetMinutes...)
}

func normalizeExplicitTargetMinutes(targetMinutes []int) []int {
	seen := make(map[int]struct{}, len(targetMinutes))
	normalized := make([]int, 0, len(targetMinutes))
	for _, minute := range targetMinutes {
		if minute <= 0 {
			continue
		}
		if _, ok := seen[minute]; ok {
			continue
		}

		seen[minute] = struct{}{}
		normalized = append(normalized, minute)
	}

	if len(normalized) == 0 {
		return nil
	}

	slices.SortFunc(normalized, func(a, b int) int { return b - a })

	return normalized
}

// minutesUntilFloorZeroClamped는 start까지 남은 시간을 분 단위로 내림 계산한다.
// 과거이거나 현재이면 0을 반환한다.
func minutesUntilFloorZeroClamped(start, now time.Time) int {
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

// NormalizeTargetMinutes는 알림 target minute 정책을 단일 규칙으로 정규화한다.
func NormalizeTargetMinutes(targetMinutes []int) []int {
	normalized := normalizeExplicitTargetMinutes(targetMinutes)
	if len(normalized) == 0 {
		return cloneDefaultTargetMinutes()
	}

	if !slices.Contains(normalized, 1) {
		normalized = append(normalized, 1)
	}

	return normalized
}

func BuildRuntimeTargetMinutes(alarmAdvanceMinutes int) []int {
	normalized := normalizeExplicitTargetMinutes([]int{alarmAdvanceMinutes})
	if len(normalized) == 0 {
		return cloneDefaultTargetMinutes()
	}

	switch minute := normalized[0]; {
	case minute <= 1:
		return []int{1}
	case minute == 2:
		return []int{2, 1}
	case minute == 3:
		return []int{3, 1}
	default:
		return []int{minute, 3, 1}
	}
}

func ResolveConfiguredTargetMinutes(targetMinutes []int) []int {
	normalized := normalizeExplicitTargetMinutes(targetMinutes)
	if len(normalized) == 0 {
		return cloneDefaultTargetMinutes()
	}
	if len(normalized) == 1 {
		return BuildRuntimeTargetMinutes(normalized[0])
	}

	return normalized
}

// IsTargetMinute는 minutesUntil 값이 targetMinutes에 포함되는지 확인한다.
func IsTargetMinute(targetMinutes []int, minutesUntil int) bool {
	return slices.Contains(targetMinutes, minutesUntil)
}

type EvaluationWindow struct {
	Start  time.Time
	End    time.Time
	Capped bool
}

func ResolveEvaluationWindow(prevCheckedAt, now time.Time, maxLookback time.Duration) EvaluationWindow {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	if maxLookback <= 0 {
		maxLookback = time.Minute
	}

	windowStart := now.Add(-maxLookback)
	window := EvaluationWindow{
		Start:  windowStart,
		End:    now,
		Capped: true,
	}

	if !prevCheckedAt.IsZero() {
		prevUTC := prevCheckedAt.UTC()
		if !prevUTC.Before(now) {
			window.Start = now.Add(-time.Second)
			return window
		}

		if prevUTC.After(windowStart) {
			window.Start = prevUTC
			window.Capped = false
		}
	}

	if !window.Start.Before(now) {
		window.Start = now.Add(-time.Second)
	}

	return window
}

func HighestCrossedTarget(targetMinutes []int, startScheduled time.Time, window EvaluationWindow) (int, bool) {
	if startScheduled.IsZero() || !window.Start.Before(window.End) {
		return 0, false
	}

	resolvedTargets := normalizeExplicitTargetMinutes(targetMinutes)
	if len(resolvedTargets) == 0 {
		resolvedTargets = cloneDefaultTargetMinutes()
	}

	current := minutesUntilFloorZeroClamped(startScheduled, window.End)
	if slices.Contains(resolvedTargets, current) {
		return current, true
	}

	if window.Capped {
		return 0, false
	}

	previous := minutesUntilFloorZeroClamped(startScheduled, window.Start)
	if previous <= current {
		return 0, false
	}

	for _, target := range resolvedTargets {
		if current < target && target <= previous {
			return target, true
		}
	}

	return 0, false
}
