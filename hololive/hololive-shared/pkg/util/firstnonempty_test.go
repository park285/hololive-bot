package util

import (
	"testing"
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
