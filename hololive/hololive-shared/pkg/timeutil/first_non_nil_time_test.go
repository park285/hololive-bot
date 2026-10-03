package timeutil

import (
	"testing"
	"time"
)

func TestFirstNonNilTime(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, time.June, 11, 12, 0, 0, 0, time.UTC)

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
