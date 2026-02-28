package notification

import "testing"

func TestBuildTargetMinutes(t *testing.T) {
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
		"alreadyContainsFallback": {
			input:    []int{15, 1, 5},
			expected: []int{15, 5, 1},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := buildTargetMinutes(tc.input)
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
