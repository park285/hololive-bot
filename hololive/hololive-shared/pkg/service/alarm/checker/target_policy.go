package checker

import (
	"slices"
	"time"
)

type TargetMinutePolicy struct {
	targetMinutes []int
}

func NewTargetMinutePolicy(targetMinutes []int) TargetMinutePolicy {
	normalized := normalizeExplicitTargetMinutes(targetMinutes)
	if len(normalized) == 0 {
		return TargetMinutePolicy{targetMinutes: cloneDefaultTargetMinutes()}
	}
	return TargetMinutePolicy{targetMinutes: append([]int(nil), normalized...)}
}

func NewTargetMinutePolicyFromRuntimeAdvance(alarmAdvanceMinutes int) TargetMinutePolicy {
	normalized := normalizeExplicitTargetMinutes([]int{alarmAdvanceMinutes})
	if len(normalized) == 0 {
		return TargetMinutePolicy{targetMinutes: cloneDefaultTargetMinutes()}
	}

	minute := normalized[0]
	switch {
	case minute <= 1:
		return TargetMinutePolicy{targetMinutes: []int{1}}
	case minute == 2:
		return TargetMinutePolicy{targetMinutes: []int{2, 1}}
	case minute == 3:
		return TargetMinutePolicy{targetMinutes: []int{3, 1}}
	default:
		return TargetMinutePolicy{targetMinutes: []int{minute, 3, 1}}
	}
}

func NewTargetMinutePolicyFromConfigured(targetMinutes []int) TargetMinutePolicy {
	normalized := normalizeExplicitTargetMinutes(targetMinutes)
	if len(normalized) == 0 {
		return TargetMinutePolicy{targetMinutes: cloneDefaultTargetMinutes()}
	}
	if len(normalized) == 1 {
		return NewTargetMinutePolicyFromRuntimeAdvance(normalized[0])
	}
	return TargetMinutePolicy{targetMinutes: append([]int(nil), normalized...)}
}

func NewTargetMinutePolicyFromPersisted(alarmAdvanceMinutes int, targetMinutes []int) TargetMinutePolicy {
	resolved := NewTargetMinutePolicyFromConfigured(targetMinutes)
	if !shouldHealLegacyPersistedTargetMinutes(alarmAdvanceMinutes, resolved.targetMinutes) {
		return resolved
	}
	return NewTargetMinutePolicyFromRuntimeAdvance(alarmAdvanceMinutes)
}

func (p TargetMinutePolicy) Clone() []int {
	if len(p.targetMinutes) == 0 {
		return cloneDefaultTargetMinutes()
	}
	return append([]int(nil), p.targetMinutes...)
}

func (p TargetMinutePolicy) Contains(minute int) bool {
	return slices.Contains(p.targetMinutes, minute)
}

func (p TargetMinutePolicy) PrimaryAdvanceMinute() int {
	if len(p.targetMinutes) == 0 {
		return cloneDefaultTargetMinutes()[0]
	}
	return p.targetMinutes[0]
}

func (p TargetMinutePolicy) HighestCrossed(startScheduled time.Time, window EvaluationWindow) (int, bool) {
	if startScheduled.IsZero() || !window.Start.Before(window.End) {
		return 0, false
	}

	resolvedTargets := p.resolvedTargetMinutes()
	current := minutesUntilFloorZeroClamped(startScheduled, window.End)
	previous := minutesUntilFloorZeroClamped(startScheduled, window.Start)
	if previous <= current {
		return currentTargetIfConfigured(resolvedTargets, current)
	}

	return highestDescendingCrossedTarget(resolvedTargets, current, previous)
}

func (p TargetMinutePolicy) resolvedTargetMinutes() []int {
	if len(p.targetMinutes) == 0 {
		return cloneDefaultTargetMinutes()
	}
	return p.targetMinutes
}

func currentTargetIfConfigured(resolvedTargets []int, current int) (int, bool) {
	if slices.Contains(resolvedTargets, current) {
		return current, true
	}
	return 0, false
}

func highestDescendingCrossedTarget(resolvedTargets []int, current int, previous int) (int, bool) {
	for _, target := range resolvedTargets {
		if current == target || (current < target && target <= previous) {
			return target, true
		}
	}

	return 0, false
}
