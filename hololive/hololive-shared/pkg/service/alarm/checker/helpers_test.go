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

package checker

import (
	"testing"
	"time"
)

func TestMinutesUntilFloor(t *testing.T) {
	now := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		start time.Time
		want  int
	}{
		{
			name:  "past_returns_zero",
			start: now.Add(-1 * time.Second),
			want:  0,
		},
		{
			name:  "same_time_returns_zero",
			start: now,
			want:  0,
		},
		{
			name:  "one_second_future_returns_zero",
			start: now.Add(1 * time.Second),
			want:  0,
		},
		{
			name:  "exactly_one_minute_returns_one",
			start: now.Add(1 * time.Minute),
			want:  1,
		},
		{
			name:  "four_minutes_thirty_seconds_floor_to_four",
			start: now.Add(4*time.Minute + 30*time.Second),
			want:  4,
		},
		{
			name:  "five_minutes_fifty_nine_seconds_floor_to_five",
			start: now.Add(5*time.Minute + 59*time.Second),
			want:  5,
		},
		{
			name:  "exactly_five_minutes_returns_five",
			start: now.Add(5 * time.Minute),
			want:  5,
		},
		{
			name:  "four_minutes_fifty_nine_seconds_floor_to_four",
			start: now.Add(4*time.Minute + 59*time.Second),
			want:  4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MinutesUntilFloor(tt.start, now)
			if got != tt.want {
				t.Fatalf("MinutesUntilFloor() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFormatScheduleChangeMessage(t *testing.T) {
	base := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		oldTime time.Time
		newTime time.Time
		want    string
	}{
		{
			name:    "delayed_when_new_is_later",
			oldTime: base,
			newTime: base.Add(30 * time.Minute),
			want:    "일정이 늦춰졌습니다.",
		},
		{
			name:    "early_when_new_is_earlier",
			oldTime: base,
			newTime: base.Add(-30 * time.Minute),
			want:    "일정이 앞당겨졌습니다.",
		},
		{
			name:    "empty_when_same_time",
			oldTime: base,
			newTime: base,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatScheduleChangeMessage(tt.oldTime, tt.newTime)
			if got != tt.want {
				t.Fatalf("FormatScheduleChangeMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeTargetMinutes(t *testing.T) {
	tests := []struct {
		name  string
		input []int
		want  []int
	}{
		{
			name:  "nil uses defaults",
			input: nil,
			want:  []int{5, 3, 1},
		},
		{
			name:  "filters duplicates and invalid values",
			input: []int{10, 0, 10, -1, 3},
			want:  []int{10, 3, 1},
		},
		{
			name:  "keeps fallback minute once",
			input: []int{15, 1, 5},
			want:  []int{15, 5, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTargetMinutes(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("NormalizeTargetMinutes() len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("NormalizeTargetMinutes() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestIsTargetMinute(t *testing.T) {
	tests := []struct {
		name          string
		targetMinutes []int
		minutesUntil  int
		want          bool
	}{
		{
			name:          "in_list_true",
			targetMinutes: []int{5, 3, 1},
			minutesUntil:  5,
			want:          true,
		},
		{
			name:          "not_in_list_false",
			targetMinutes: []int{5, 3, 1},
			minutesUntil:  2,
			want:          false,
		},
		{
			name:          "zero_minute_not_in_list_false",
			targetMinutes: []int{5, 3, 1},
			minutesUntil:  0,
			want:          false,
		},
		{
			name:          "empty_list_false",
			targetMinutes: nil,
			minutesUntil:  5,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTargetMinute(tt.targetMinutes, tt.minutesUntil)
			if got != tt.want {
				t.Fatalf("IsTargetMinute() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCrossedTarget(t *testing.T) {
	base := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)
	start := base.Add(5*time.Minute + 10*time.Second)

	tests := []struct {
		name    string
		targets []int
		prev    time.Time
		now     time.Time
		want    int
		wantOK  bool
	}{
		{
			name:    "exact current target wins",
			targets: []int{5, 3, 1},
			prev:    base.Add(2 * time.Second),
			now:     base.Add(10 * time.Second),
			want:    5,
			wantOK:  true,
		},
		{
			name:    "crossed target returns missed boundary",
			targets: []int{5, 3, 1},
			prev:    base,
			now:     base.Add(50 * time.Second),
			want:    5,
			wantOK:  true,
		},
		{
			name:    "no previous tick means no crossed fallback",
			targets: []int{5, 3, 1},
			prev:    time.Time{},
			now:     base.Add(50 * time.Second),
			want:    0,
			wantOK:  false,
		},
		{
			name:    "older target outside window is ignored",
			targets: []int{5, 3, 1},
			prev:    base.Add(70 * time.Second),
			now:     base.Add(130 * time.Second),
			want:    3,
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CrossedTarget(tt.targets, start, tt.prev, tt.now)
			if ok != tt.wantOK {
				t.Fatalf("CrossedTarget() ok = %t, want %t", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("CrossedTarget() minute = %d, want %d", got, tt.want)
			}
		})
	}
}
