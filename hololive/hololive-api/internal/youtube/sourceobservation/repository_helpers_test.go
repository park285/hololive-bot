package sourceobservation

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedErrorDetailUTF8Boundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: ""},
		{name: "ascii at limit", value: strings.Repeat("a", maxErrorTextBytes), want: strings.Repeat("a", maxErrorTextBytes)},
		{name: "ascii over limit", value: strings.Repeat("a", maxErrorTextBytes+1), want: strings.Repeat("a", maxErrorTextBytes)},
		{name: "3-byte rune across limit", value: strings.Repeat("가", 683), want: strings.Repeat("가", 682)},
		{name: "4-byte rune across limit", value: strings.Repeat("a", maxErrorTextBytes-2) + "😀", want: strings.Repeat("a", maxErrorTextBytes-2)},
		{name: "rune ends at limit", value: strings.Repeat("a", maxErrorTextBytes-4) + "😀" + "b", want: strings.Repeat("a", maxErrorTextBytes-4) + "😀"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := BoundedErrorDetail(tt.value)
			if got != tt.want || !utf8.ValidString(got) {
				t.Fatalf("BoundedErrorDetail() = %d bytes (valid UTF-8 %t), want %d bytes", len(got), utf8.ValidString(got), len(tt.want))
			}
		})
	}
}
