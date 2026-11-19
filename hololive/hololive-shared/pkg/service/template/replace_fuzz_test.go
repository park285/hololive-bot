package template

import (
	"errors"
	"strings"
	"testing"
)

func FuzzBoundedReplace(f *testing.F) {
	f.Add("공지: {이름}", "{이름}", "멤버")
	f.Add("한\xff", "", "|")
	f.Add("", "", "")
	f.Add(strings.Repeat("a", 256), "a", strings.Repeat("b", 256))
	f.Add(strings.Repeat("a", 257), "a", strings.Repeat("b", 256))

	f.Fuzz(func(t *testing.T, text, old, replacement string) {
		if len(text) > 512 || len(old) > 512 || len(replacement) > 512 {
			t.Skip()
		}

		want := strings.ReplaceAll(text, old, replacement)
		got, err := boundedReplace(text, old, replacement)

		if len(want) > templateOutputMaxBytes {
			if !errors.Is(err, ErrTemplateExecutionLimit) || got != "" {
				t.Fatalf("oversized output: size %d error %v, want empty result and limit error", len(got), err)
			}
		} else if err != nil || got != want {
			t.Fatalf("replacement mismatch: got %d bytes, %v; want %d bytes", len(got), err, len(want))
		}
	})
}
