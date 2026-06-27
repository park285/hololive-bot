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

package scraper

import (
	"testing"
	"time"
)

func TestCalculateNextRunAtHour(t *testing.T) {
	now := time.Date(2026, time.March, 4, 18, 0, 0, 0, time.UTC) // 03:00 KST

	next := calculateNextRunAtHour(now, 4)
	want := time.Date(2026, time.March, 4, 19, 0, 0, 0, time.UTC) // 04:00 KST

	if !next.Equal(want) {
		t.Fatalf("calculateNextRunAtHour() = %s, want %s", next, want)
	}
}

func TestBuildRetryRuns(t *testing.T) {
	baseRun := time.Date(2026, time.March, 4, 19, 0, 0, 0, time.UTC)  // KST 04:00
	failedAt := time.Date(2026, time.March, 4, 19, 5, 0, 0, time.UTC) // KST 04:05
	crossDay := 21 * time.Hour                                        // KST +21h -> next day 01:00
	retries := buildRetryRuns(baseRun, failedAt, []time.Duration{30 * time.Minute, 2 * time.Hour, crossDay})

	if len(retries) != 2 {
		t.Fatalf("buildRetryRuns() len = %d, want 2", len(retries))
	}
}
