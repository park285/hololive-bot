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
