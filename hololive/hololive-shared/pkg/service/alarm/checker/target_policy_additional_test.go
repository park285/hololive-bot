package checker

import (
	"testing"
	"time"
)

func TestResolveEvaluationWindow_InitialObservation(t *testing.T) {
	now := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)

	window := ResolveEvaluationWindow(time.Time{}, now, 75*time.Second)
	if !window.InitialObservation {
		t.Fatalf("ResolveEvaluationWindow() initial observation = false, want true")
	}

	recent := ResolveEvaluationWindow(now.Add(-30*time.Second), now, 75*time.Second)
	if recent.InitialObservation {
		t.Fatalf("ResolveEvaluationWindow() initial observation = true, want false")
	}
}

func TestTargetMinutePolicy_HighestCrossed_InitialCappedObservationBackfills(t *testing.T) {
	base := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)
	policy := NewTargetMinutePolicy([]int{5, 3, 1})
	window := EvaluationWindow{
		Start:              base.Add(-75 * time.Second),
		End:                base,
		Capped:             true,
		InitialObservation: true,
	}

	got, ok := policy.HighestCrossed(base.Add(4*time.Minute+20*time.Second), window)
	if !ok || got != 5 {
		t.Fatalf("TargetMinutePolicy.HighestCrossed() = (%d, %t), want (5, true)", got, ok)
	}
}

func TestTargetMinutePolicy_HighestCrossed_CappedAfterInitialObservationRecoversRecentCrossing(t *testing.T) {
	base := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)
	policy := NewTargetMinutePolicy([]int{5, 3, 1})
	window := EvaluationWindow{
		Start:              base.Add(-75 * time.Second),
		End:                base,
		Capped:             true,
		InitialObservation: false,
	}

	got, ok := policy.HighestCrossed(base.Add(4*time.Minute), window)
	if !ok || got != 5 {
		t.Fatalf("TargetMinutePolicy.HighestCrossed() = (%d, %t), want (5, true)", got, ok)
	}
}

func TestTargetMinutePolicy_HighestCrossed_CappedAfterInitialObservationUsesLowerRecentFallback(t *testing.T) {
	base := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)
	policy := NewTargetMinutePolicy([]int{5, 3, 1})
	window := EvaluationWindow{
		Start:              base.Add(-75 * time.Second),
		End:                base,
		Capped:             true,
		InitialObservation: false,
	}

	got, ok := policy.HighestCrossed(base.Add(3*time.Minute+20*time.Second), window)
	if !ok || got != 3 {
		t.Fatalf("TargetMinutePolicy.HighestCrossed() = (%d, %t), want (3, true)", got, ok)
	}
}

func TestTargetMinutePolicy_HighestCrossed_PrefersHigherRecentCrossingOverCurrentLowerTarget(t *testing.T) {
	base := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)
	policy := NewTargetMinutePolicy([]int{5, 3, 1})
	window := EvaluationWindow{
		Start:              base.Add(-75 * time.Second),
		End:                base,
		Capped:             true,
		InitialObservation: false,
	}

	got, ok := policy.HighestCrossed(base.Add(3*time.Minute+59*time.Second), window)
	if !ok || got != 5 {
		t.Fatalf("TargetMinutePolicy.HighestCrossed() = (%d, %t), want (5, true)", got, ok)
	}
}

func TestTargetMinutePolicy_HighestCrossed_CappedRecoveryThresholds(t *testing.T) {
	base := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)
	policy := NewTargetMinutePolicy([]int{5, 3, 1})
	window := EvaluationWindow{
		Start:              base.Add(-75 * time.Second),
		End:                base,
		Capped:             true,
		InitialObservation: false,
	}

	tests := []struct {
		name      string
		remaining time.Duration
		want      int
	}{
		{
			name:      "recovers five at four fifty nine",
			remaining: 4*time.Minute + 59*time.Second,
			want:      5,
		},
		{
			name:      "recovers five at four minutes",
			remaining: 4 * time.Minute,
			want:      5,
		},
		{
			name:      "recovers five at three fifty nine",
			remaining: 3*time.Minute + 59*time.Second,
			want:      5,
		},
		{
			name:      "recovers five at three forty five",
			remaining: 3*time.Minute + 45*time.Second,
			want:      5,
		},
		{
			name:      "falls back to three once five is outside cap",
			remaining: 3*time.Minute + 44*time.Second,
			want:      3,
		},
		{
			name:      "uses three when five is stale",
			remaining: 3*time.Minute + 20*time.Second,
			want:      3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := policy.HighestCrossed(base.Add(tt.remaining), window)
			if !ok || got != tt.want {
				t.Fatalf("TargetMinutePolicy.HighestCrossed() = (%d, %t), want (%d, true)", got, ok, tt.want)
			}
		})
	}
}

func TestTargetMinutePolicy_HighestCrossed_CappedWindowDoesNotRecoverOldTarget(t *testing.T) {
	base := time.Date(2026, 4, 14, 3, 0, 0, 0, time.UTC)
	policy := NewTargetMinutePolicy([]int{5})
	window := EvaluationWindow{
		Start:              base.Add(-75 * time.Second),
		End:                base,
		Capped:             true,
		InitialObservation: false,
	}

	got, ok := policy.HighestCrossed(base.Add(3*time.Minute+20*time.Second), window)
	if ok || got != 0 {
		t.Fatalf("TargetMinutePolicy.HighestCrossed() = (%d, %t), want (0, false)", got, ok)
	}
}

func TestTargetMinutePolicy_Constructors(t *testing.T) {
	configured := NewTargetMinutePolicyFromConfigured([]int{15, 15, 5, 0})
	if got := configured.Clone(); len(got) != 2 || got[0] != 15 || got[1] != 5 {
		t.Fatalf("configured.Clone() = %v, want [15 5]", got)
	}

	runtime := NewTargetMinutePolicyFromRuntimeAdvance(5)
	if got := runtime.Clone(); len(got) != 3 || got[0] != 5 || got[1] != 3 || got[2] != 1 {
		t.Fatalf("runtime.Clone() = %v, want [5 3 1]", got)
	}

	persisted := NewTargetMinutePolicyFromPersisted(5, []int{5, 1})
	if got := persisted.Clone(); len(got) != 3 || got[0] != 5 || got[1] != 3 || got[2] != 1 {
		t.Fatalf("persisted.Clone() = %v, want [5 3 1]", got)
	}

	if runtime.PrimaryAdvanceMinute() != 5 {
		t.Fatalf("runtime.PrimaryAdvanceMinute() = %d, want 5", runtime.PrimaryAdvanceMinute())
	}
	if !runtime.Contains(3) || runtime.Contains(2) {
		t.Fatalf("runtime.Contains() returned unexpected result")
	}
}
