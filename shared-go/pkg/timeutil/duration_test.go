package timeutil

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45초"},
		{5 * time.Minute, "5분"},
		{5*time.Minute + 30*time.Second, "5분 30초"},
		{2 * time.Hour, "2시간"},
		{2*time.Hour + 15*time.Minute, "2시간 15분"},
		{Day, "1일"},
		{Day + 3*time.Hour, "1일 3시간"},
		{3 * Day, "3일"},
		{0, "0초"},
		{-1 * time.Hour, "0초"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.d)
		if got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestFormatDurationCompact(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{5 * time.Minute, "5m"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
		{Day, "1d"},
		{Day + 5*time.Hour, "1d5h"},
	}

	for _, tt := range tests {
		got := FormatDurationCompact(tt.d)
		if got != tt.want {
			t.Errorf("FormatDurationCompact(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTruncateToDay(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Seoul")
	input := time.Date(2024, 1, 15, 14, 30, 45, 123, loc)
	want := time.Date(2024, 1, 15, 0, 0, 0, 0, loc)

	got := TruncateToDay(input)
	if !got.Equal(want) {
		t.Errorf("TruncateToDay() = %v, want %v", got, want)
	}
}
