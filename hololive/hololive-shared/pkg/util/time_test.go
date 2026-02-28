package util

import (
	"testing"
	"time"
)

func TestMinutesUntilCeil(t *testing.T) {
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
		"ceil boundary": {
			target:   new(now.Add(4*time.Minute + 1*time.Second)),
			expected: 5,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := MinutesUntilCeil(tc.target, now); got != tc.expected {
				t.Fatalf("MinutesUntilCeil() = %d, expected %d", got, tc.expected)
			}
		})
	}
}

//go:fix inline
func ptr(t time.Time) *time.Time {
	return new(t)
}
