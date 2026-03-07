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

package youtube

import "testing"

func TestShouldReturnFallbackError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		currentResults    int
		failedTargets     int
		fallbackSuccesses int
		want              bool
	}{
		{
			name:              "returns true when all primary targets failed and fallback had no successes",
			currentResults:    0,
			failedTargets:     3,
			fallbackSuccesses: 0,
			want:              true,
		},
		{
			name:              "returns false when partial primary results exist",
			currentResults:    2,
			failedTargets:     3,
			fallbackSuccesses: 0,
			want:              false,
		},
		{
			name:              "returns false when fallback succeeded but no items were produced",
			currentResults:    0,
			failedTargets:     3,
			fallbackSuccesses: 1,
			want:              false,
		},
		{
			name:              "returns false when there were no failed targets",
			currentResults:    0,
			failedTargets:     0,
			fallbackSuccesses: 0,
			want:              false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shouldReturnFallbackError(tt.currentResults, tt.failedTargets, tt.fallbackSuccesses)
			if got != tt.want {
				t.Fatalf("shouldReturnFallbackError(%d, %d, %d) = %v, want %v",
					tt.currentResults, tt.failedTargets, tt.fallbackSuccesses, got, tt.want)
			}
		})
	}
}
