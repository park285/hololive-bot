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

package util

import (
	"testing"
	"time"
)

func TestMinutesUntilFloor(t *testing.T) {
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
			if got := MinutesUntilFloor(tc.target, now); got != tc.expected {
				t.Fatalf("MinutesUntilFloor() = %d, expected %d", got, tc.expected)
			}
		})
	}
}
