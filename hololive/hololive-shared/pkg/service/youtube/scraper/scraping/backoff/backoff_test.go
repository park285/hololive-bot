package backoff

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHardCooldownForErrorCount_Thresholds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		errors int
		want   time.Duration
	}{
		{errors: -1, want: 30 * time.Minute},
		{errors: 0, want: 30 * time.Minute},
		{errors: 1, want: 30 * time.Minute},
		{errors: 2, want: 1 * time.Hour},
		{errors: 3, want: 2 * time.Hour},
		{errors: 4, want: 4 * time.Hour},
		{errors: 5, want: 6 * time.Hour},
		{errors: 6, want: 6 * time.Hour},
		{errors: 100, want: 6 * time.Hour},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, hardCooldownForErrorCount(tc.errors), "errors=%d", tc.errors)
	}
}

func TestTransientCooldownForErrorCount_Thresholds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		errors int
		want   time.Duration
	}{
		{errors: -1, want: 30 * time.Second},
		{errors: 0, want: 30 * time.Second},
		{errors: 1, want: 30 * time.Second},
		{errors: 2, want: 3 * time.Minute},
		{errors: 3, want: 10 * time.Minute},
		{errors: 10, want: 10 * time.Minute},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, transientCooldownForErrorCount(tc.errors), "errors=%d", tc.errors)
	}
}

func TestClampCooldown_Bounds(t *testing.T) {
	t.Parallel()

	minValue := 5 * time.Second
	maxValue := 10 * time.Minute

	require.Equal(t, minValue, clampCooldown(time.Second, minValue, maxValue))
	require.Equal(t, minValue, clampCooldown(minValue, minValue, maxValue))
	require.Equal(t, 30*time.Second, clampCooldown(30*time.Second, minValue, maxValue))
	require.Equal(t, maxValue, clampCooldown(maxValue, minValue, maxValue))
	require.Equal(t, maxValue, clampCooldown(time.Hour, minValue, maxValue))
}

func TestResolveCooldown_SuggestedInteraction(t *testing.T) {
	t.Parallel()

	base := 30 * time.Minute
	minValue := 30 * time.Second
	maxValue := 6 * time.Hour

	require.Equal(t, base, resolveCooldown(base, 0, minValue, maxValue))
	require.Equal(t, base, resolveCooldown(base, -time.Hour, minValue, maxValue))

	require.Equal(t, base, resolveCooldown(base, time.Second, minValue, maxValue))

	require.Equal(t, base, resolveCooldown(base, 20*time.Minute, minValue, maxValue))

	require.Equal(t, 45*time.Minute, resolveCooldown(base, 45*time.Minute, minValue, maxValue))

	require.Equal(t, maxValue, resolveCooldown(base, 24*time.Hour, minValue, maxValue))
}

func TestLaterDeadline_ReturnsLaterOrCandidateOnTie(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	earlier := base.Add(-time.Hour)
	later := base.Add(time.Hour)

	require.Equal(t, base, laterDeadline(base, earlier))
	require.Equal(t, later, laterDeadline(base, later))
	require.Equal(t, base, laterDeadline(base, base))
}

func TestNewBackoffState_DefaultHasNoJitter(t *testing.T) {
	t.Parallel()

	bs := NewBackoffState()
	require.Equal(t, 0.0, bs.jitterPortion)
	require.Nil(t, bs.jitterRNG)
	require.False(t, bs.IsInCooldown())
}

func TestWithCooldownJitter_ClampsPortionAndAllocatesRNG(t *testing.T) {
	t.Parallel()

	t.Run("negative portion clamps to zero and leaves RNG nil", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState(WithCooldownJitter(-0.3))
		require.Equal(t, 0.0, bs.jitterPortion)
		require.Nil(t, bs.jitterRNG)
	})

	t.Run("zero portion leaves RNG nil", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState(WithCooldownJitter(0))
		require.Equal(t, 0.0, bs.jitterPortion)
		require.Nil(t, bs.jitterRNG)
	})

	t.Run("in-range portion allocates RNG", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState(WithCooldownJitter(0.3))
		require.Equal(t, 0.3, bs.jitterPortion)
		require.NotNil(t, bs.jitterRNG)
	})

	t.Run("portion above cap clamps to max and allocates RNG", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState(WithCooldownJitter(0.9))
		require.Equal(t, maxCooldownJitterPortion, bs.jitterPortion)
		require.NotNil(t, bs.jitterRNG)
	})
}

func TestApplyJitter_NoOpWhenDisabledOrNonPositive(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5*time.Minute, NewBackoffState().applyJitter(5*time.Minute))

	jittered := NewBackoffState(WithCooldownJitter(0.5))
	require.Equal(t, time.Duration(0), jittered.applyJitter(0))
	require.Equal(t, -time.Minute, jittered.applyJitter(-time.Minute))
}

func TestApplyJitter_StaysWithinPortionBounds(t *testing.T) {
	t.Parallel()

	bs := NewBackoffState(WithCooldownJitter(0.5))
	bs.jitterRNG = rand.New(rand.NewSource(1)) //nolint:gosec // G404: 고정 시드로 지터 범위를 결정적으로 검증하며, jitterRNG 필드 타입이 *math/rand.Rand

	cooldown := 10 * time.Minute
	lower := time.Duration(float64(cooldown) * 0.5)
	upper := time.Duration(float64(cooldown) * 1.5)
	for range 2000 {
		got := bs.applyJitter(cooldown)
		require.GreaterOrEqual(t, got, lower)
		require.Less(t, got, upper)
	}
}

func TestRecordError_HardCooldownEscalatesWithErrorCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		calls int
		want  time.Duration
	}{
		{calls: 1, want: 30 * time.Minute},
		{calls: 2, want: 1 * time.Hour},
		{calls: 3, want: 2 * time.Hour},
		{calls: 4, want: 4 * time.Hour},
		{calls: 5, want: 6 * time.Hour},
	}
	for _, tc := range cases {
		bs := NewBackoffState()
		for i := 0; i < tc.calls; i++ {
			bs.RecordError()
		}
		remaining := bs.HardCooldownRemaining()
		require.LessOrEqual(t, remaining, tc.want, "calls=%d", tc.calls)
		require.Greater(t, remaining, tc.want-2*time.Second, "calls=%d", tc.calls)
		require.True(t, bs.IsInCooldown())
	}
}

func TestRecordTransientError_EscalatesWithErrorCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		calls int
		want  time.Duration
	}{
		{calls: 1, want: 30 * time.Second},
		{calls: 2, want: 3 * time.Minute},
		{calls: 3, want: 10 * time.Minute},
	}
	for _, tc := range cases {
		bs := NewBackoffState()
		for i := 0; i < tc.calls; i++ {
			bs.RecordTransientError()
		}
		require.Equal(t, tc.calls, bs.TransientErrors())
		remaining := bs.TransientCooldownRemaining()
		require.LessOrEqual(t, remaining, tc.want, "calls=%d", tc.calls)
		require.Greater(t, remaining, tc.want-2*time.Second, "calls=%d", tc.calls)
	}
}

func TestRecordErrorWithSuggestedCooldown_TakesMaxOfBaseAndClampedSuggested(t *testing.T) {
	t.Parallel()

	t.Run("large suggested is clamped to max and dominates base", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState()
		bs.RecordErrorWithSuggestedCooldown(24 * time.Hour)
		remaining := bs.HardCooldownRemaining()
		require.LessOrEqual(t, remaining, 6*time.Hour)
		require.Greater(t, remaining, 6*time.Hour-2*time.Second)
	})

	t.Run("small suggested is dominated by base cooldown", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState()
		bs.RecordErrorWithSuggestedCooldown(time.Second)
		remaining := bs.HardCooldownRemaining()
		require.LessOrEqual(t, remaining, 30*time.Minute)
		require.Greater(t, remaining, 30*time.Minute-2*time.Second)
	})
}

func TestRecordTransientErrorWithSuggestedCooldown_ClampsToTransientRange(t *testing.T) {
	t.Parallel()

	bs := NewBackoffState()
	bs.RecordTransientErrorWithSuggestedCooldown(time.Hour)
	remaining := bs.TransientCooldownRemaining()
	require.LessOrEqual(t, remaining, 10*time.Minute)
	require.Greater(t, remaining, 10*time.Minute-2*time.Second)
}

func TestRecordError_DeadlineNeverShrinksAcrossCalls(t *testing.T) {
	t.Parallel()

	bs := NewBackoffState()
	bs.RecordErrorWithSuggestedCooldown(24 * time.Hour)
	first := bs.HardCooldownRemaining()

	bs.RecordError()
	second := bs.HardCooldownRemaining()

	require.LessOrEqual(t, second, first)
	require.Greater(t, second, 6*time.Hour-2*time.Second)
}

func TestRecordSuccess_ResetsAllState(t *testing.T) {
	t.Parallel()

	bs := NewBackoffState()
	bs.RecordError()
	bs.RecordError()
	bs.RecordTransientError()
	require.True(t, bs.IsInCooldown())

	bs.RecordSuccess()

	require.Equal(t, time.Duration(0), bs.HardCooldownRemaining())
	require.Equal(t, time.Duration(0), bs.TransientCooldownRemaining())
	require.Equal(t, time.Duration(0), bs.CooldownRemaining())
	require.False(t, bs.IsInCooldown())
	require.Equal(t, 0, bs.TransientErrors())
	require.Equal(t, 0, bs.hardErrors)
}

func TestHardCooldownRemaining_ExpiredResetsCounters(t *testing.T) {
	t.Parallel()

	t.Run("zero deadline returns zero", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState()
		require.Equal(t, time.Duration(0), bs.HardCooldownRemaining())
	})

	t.Run("past deadline resets deadline and error count", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState()
		bs.hardErrors = 3
		bs.hardCooldown = time.Now().Add(-time.Minute)
		require.Equal(t, time.Duration(0), bs.HardCooldownRemaining())
		require.True(t, bs.hardCooldown.IsZero())
		require.Equal(t, 0, bs.hardErrors)
	})
}

func TestTransientCooldownRemaining_ExpiredResetsCounters(t *testing.T) {
	t.Parallel()

	t.Run("future deadline reports positive remaining", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState()
		bs.SetTransientCooldownForTest(time.Now().Add(2 * time.Minute))
		remaining := bs.TransientCooldownRemaining()
		require.Greater(t, remaining, time.Duration(0))
		require.LessOrEqual(t, remaining, 2*time.Minute)
	})

	t.Run("past deadline resets deadline and error count", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState()
		bs.transientErrors = 2
		bs.SetTransientCooldownForTest(time.Now().Add(-time.Minute))
		require.Equal(t, time.Duration(0), bs.TransientCooldownRemaining())
		require.Equal(t, 0, bs.TransientErrors())
	})
}

func TestCooldownRemaining_ReturnsLargerOfHardAndTransient(t *testing.T) {
	t.Parallel()

	t.Run("hard dominates", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState()
		bs.hardCooldown = time.Now().Add(10 * time.Minute)
		bs.transientCooldown = time.Now().Add(5 * time.Minute)
		remaining := bs.CooldownRemaining()
		require.Greater(t, remaining, 5*time.Minute)
		require.LessOrEqual(t, remaining, 10*time.Minute)
	})

	t.Run("transient dominates", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState()
		bs.hardCooldown = time.Now().Add(2 * time.Minute)
		bs.transientCooldown = time.Now().Add(8 * time.Minute)
		remaining := bs.CooldownRemaining()
		require.Greater(t, remaining, 2*time.Minute)
		require.LessOrEqual(t, remaining, 8*time.Minute)
	})

	t.Run("expired hard falls back to live transient", func(t *testing.T) {
		t.Parallel()
		bs := NewBackoffState()
		bs.hardErrors = 3
		bs.hardCooldown = time.Now().Add(-time.Minute)
		bs.transientCooldown = time.Now().Add(4 * time.Minute)
		remaining := bs.CooldownRemaining()
		require.Greater(t, remaining, time.Duration(0))
		require.LessOrEqual(t, remaining, 4*time.Minute)
		require.True(t, bs.hardCooldown.IsZero())
		require.Equal(t, 0, bs.hardErrors)
	})
}

func TestIsInCooldown_TracksRecordAndSuccess(t *testing.T) {
	t.Parallel()

	bs := NewBackoffState()
	require.False(t, bs.IsInCooldown())
	bs.RecordError()
	require.True(t, bs.IsInCooldown())
	bs.RecordSuccess()
	require.False(t, bs.IsInCooldown())
}
