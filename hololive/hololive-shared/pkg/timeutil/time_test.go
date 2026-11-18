package timeutil

import (
	"testing"
	"time"
)

func TestMinutesUntilFloorPtr(t *testing.T) {
	t.Parallel()

	now := time.Now()

	cases := map[string]struct {
		target   *time.Time
		expected int
	}{
		"nil target": {
			target:   nil,
			expected: -1,
		},
		"past target": {
			target:   new(now.Add(-1 * time.Minute)),
			expected: -1,
		},
		"exact minutes ahead": {
			target:   new(now.Add(5 * time.Minute)),
			expected: 5,
		},
		"floor boundary": {
			target:   new(now.Add(4*time.Minute + 1*time.Second)),
			expected: 4,
		},
		"5min 59sec ahead": {
			target:   new(now.Add(5*time.Minute + 59*time.Second)),
			expected: 5,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := MinutesUntilFloorPtr(tc.target, now); got != tc.expected {
				t.Fatalf("MinutesUntilFloorPtr() = %d, expected %d", got, tc.expected)
			}
		})
	}
}

func TestMinutesUntilCeilPtr(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		target *time.Time
		want   int
	}{
		"nil":            {target: nil, want: -1},
		"past":           {target: new(now.Add(-time.Second)), want: -1},
		"now":            {target: new(now), want: -1},
		"under minute":   {target: new(now.Add(time.Second)), want: 1},
		"exact minutes":  {target: new(now.Add(30 * time.Minute)), want: 30},
		"partial minute": {target: new(now.Add(30*time.Minute + time.Second)), want: 31},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := MinutesUntilCeilPtr(test.target, now); got != test.want {
				t.Fatalf("MinutesUntilCeilPtr() = %d, want %d", got, test.want)
			}
		})
	}
}
