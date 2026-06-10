package util

import (
	"testing"
	"time"
)

func TestFirstNonEmptyString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "empty input", values: nil, want: ""},
		{name: "all empty", values: []string{"", "", ""}, want: ""},
		{name: "first non-empty", values: []string{"a", "b"}, want: "a"},
		{name: "skip leading empties", values: []string{"", "", "c"}, want: "c"},
		{name: "skip whitespace-only", values: []string{"   ", "\t", "value"}, want: "value"},
		{name: "returns original untrimmed value", values: []string{"  padded  "}, want: "  padded  "},
		{name: "single empty", values: []string{""}, want: ""},
		{name: "single whitespace", values: []string{"  "}, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FirstNonEmptyString(tc.values...); got != tc.want {
				t.Fatalf("FirstNonEmptyString(%q) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

func TestFirstNonNilTime(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		values []*time.Time
		want   *time.Time
	}{
		{name: "empty input", values: nil, want: nil},
		{name: "all nil", values: []*time.Time{nil, nil}, want: nil},
		{name: "first non-nil", values: []*time.Time{&t1, &t2}, want: &t1},
		{name: "skip leading nil", values: []*time.Time{nil, &t2}, want: &t2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FirstNonNilTime(tc.values...); got != tc.want {
				t.Fatalf("FirstNonNilTime(%v) = %v, want %v", tc.values, got, tc.want)
			}
		})
	}
}
