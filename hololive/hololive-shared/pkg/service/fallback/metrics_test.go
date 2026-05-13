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

package fallback

import "testing"

func TestPrimaryOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		attempted int
		succeeded int
		failed    int
		want      string
	}{
		{name: "skipped", attempted: 0, succeeded: 0, failed: 0, want: "skipped"},
		{name: "success", attempted: 3, succeeded: 3, failed: 0, want: "success"},
		{name: "partial", attempted: 3, succeeded: 2, failed: 1, want: "partial"},
		{name: "empty", attempted: 3, succeeded: 0, failed: 0, want: "empty"},
		{name: "failed", attempted: 3, succeeded: 0, failed: 3, want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := primaryOutcome(tt.attempted, tt.succeeded, tt.failed); got != tt.want {
				t.Fatalf("primaryOutcome() = %q, want %q", got, tt.want)
			}
		})
	}
}
