package targetpolicy

import (
	"slices"
	"time"
)

var defaultTargetMinutes = [...]int{5, 3, 1}

func cloneDefaultTargetMinutes() []int {
	return slices.Clone(defaultTargetMinutes[:])
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

func MinutesUntilFloorZeroClamped(start, now time.Time) int {
	secs := start.Sub(now) / time.Second
	if secs <= 0 {
		return 0
	}

	return int(secs / 60)
}

func FormatScheduleChangeMessage(oldTime, newTime time.Time) string {
	if oldTime.Before(newTime) {
		return "일정이 늦춰졌습니다."
	}

	if oldTime.After(newTime) {
		return "일정이 앞당겨졌습니다."
	}

	return ""
}

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
	return NewTargetMinutePolicyFromRuntimeAdvance(alarmAdvanceMinutes).Clone()
}

type EvaluationWindow struct {
	Start              time.Time
	End                time.Time
	Capped             bool
	InitialObservation bool
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
		Start:              windowStart,
		End:                now,
		Capped:             true,
		InitialObservation: prevCheckedAt.IsZero(),
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
