package subscriptions

import (
	"testing"

	targetpolicy "github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
)

func TestNormalizeTargetMinutes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input    []int
		expected []int
	}{
		"defaultSeed": {
			input:    nil,
			expected: []int{5, 3, 1},
		},
		"customDescendingKeepsOrderAndAddsFallback": {
			input:    []int{55, 30, 10},
			expected: []int{55, 30, 10, 1},
		},
		"filtersInvalidAndDuplicates": {
			input:    []int{10, 0, 10, -5, 3, 1},
			expected: []int{10, 3, 1},
		},
		"singleExplicitFiveStillAddsRuntimeFallback": {
			input:    []int{5},
			expected: []int{5, 1},
		},
		"alreadyContainsFallback": {
			input:    []int{15, 1, 5},
			expected: []int{15, 5, 1},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := targetpolicy.NormalizeTargetMinutes(tc.input)
			if len(got) != len(tc.expected) {
				t.Fatalf("unexpected length: got=%v expected=%v", got, tc.expected)
			}

			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("unexpected targets: got=%v expected=%v", got, tc.expected)
				}
			}
		})
	}
}

func TestBuildRuntimeTargetMinutes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input    int
		expected []int
	}{
		"ten":   {input: 10, expected: []int{10, 3, 1}},
		"five":  {input: 5, expected: []int{5, 3, 1}},
		"three": {input: 3, expected: []int{3, 1}},
		"two":   {input: 2, expected: []int{2, 1}},
		"one":   {input: 1, expected: []int{1}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := targetpolicy.BuildRuntimeTargetMinutes(tc.input)
			if len(got) != len(tc.expected) {
				t.Fatalf("unexpected length: got=%v expected=%v", got, tc.expected)
			}

			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("unexpected targets: got=%v expected=%v", got, tc.expected)
				}
			}
		})
	}
}
