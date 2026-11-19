package sourceobservation

import (
	"encoding/json/jsontext"
	"slices"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func FuzzCanonicalNameUTF16Order(f *testing.F) {
	f.Add("", "")
	f.Add("a", "a\x00")
	f.Add("공통\U00010000", "공통\ue000")
	f.Add("\U000103ff", "\U00010400")
	f.Add("\U0010fffe", "\U0010ffff")
	f.Add("\ud7ff", "\U00010000")
	f.Add("\uffff", "\U0010ffff")
	f.Add("\"", "\\")
	f.Fuzz(func(t *testing.T, left, right string) {
		if !utf8.ValidString(left) || !utf8.ValidString(right) {
			t.Skip("JSON member names require valid UTF-8")
		}

		want := slices.Compare(utf16.Encode([]rune(left)), utf16.Encode([]rune(right)))
		if got := compareJSONNamesUTF16([]byte(left), []byte(right)); got != want {
			t.Fatalf("compareJSONNamesUTF16(%q, %q) = %d, want %d", left, right, got, want)
		}

		if want == 0 {
			return
		}

		a := fuzzQuotedMember(t, left, '1')
		b := fuzzQuotedMember(t, right, '2')

		if want > 0 {
			a, b = b, a
		}

		ordered := "{" + a + "," + b + "}"
		if len(ordered) > MaxPayloadBytes {
			return
		}

		for _, input := range []string{ordered, "{" + b + "," + a + "}"} {
			got, err := CanonicalizeJSON([]byte(input))
			if err != nil || string(got) != ordered {
				t.Fatalf("CanonicalizeJSON(%s) = (%s, %v), want %s", input, got, err, ordered)
			}
		}
	})
}

func fuzzQuotedMember(t *testing.T, name string, digit byte) string {
	t.Helper()

	quoted, err := jsontext.AppendQuote(nil, name)
	if err != nil {
		t.Fatalf("AppendQuote(%q): %v", name, err)
	}

	return string(append(quoted, ':', digit))
}
