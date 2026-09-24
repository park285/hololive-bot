package template

import (
	"bytes"
	"strings"
	"testing"
	texttemplate "text/template"
)

const legacyDisplayLineExpression = `{{trim (replace (replace (replace (replace (replace . "\u200b" "") "\r\n" " ") "\r" " ") "\n" " ") "\t" " ")}}`

func TestDisplayLinePreservesWhitespaceContract(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""},
		{"  A\t\tB  ", "A  B"},
		{"A\r\u200b\nB", "A B"},
		{"A\r\n\r\nB", "A  B"},
		{"\u3000A\u3000B\u3000", "A\u3000B"},
		{"A\u00a0B", "A\u00a0B"},
		{" 한글\t日本語\r\n😀 ", "한글 日本語 😀"},
		{"\xff\t\xfe", "\xff \xfe"},
	} {
		if got := normalizeTemplateDisplayLine(tc.input); got != tc.want {
			t.Errorf("displayline(%q)=%q want=%q", tc.input, got, tc.want)
		}
	}
}

// 최적화 전 truncate 구현을 독립 비교 기준으로 유지합니다.
func legacyTemplateTruncate(maxLen int, s string) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	if maxLen <= 3 {
		return string(runes[:maxLen])
	}

	return string(runes[:maxLen-3]) + "..."
}

func TestTemplateTruncatePreservesLegacyBoundaries(t *testing.T) {
	for _, input := range []string{
		"", "plain", "한글 日本語 😀👨‍👩‍👧‍👦", strings.Repeat("한글😀", 50),
		"\xffa\xfeb\xfdcdefgh", "a\xff\xfe", "a\ufffdb", "abc\xe2\x82",
	} {
		for _, limit := range []int{0, 1, 2, 3, 4, 5, 63, 64, 65, 100} {
			if got, want := truncateTemplateText(limit, input), legacyTemplateTruncate(limit, input); got != want {
				t.Errorf("truncate(%d,%q)=%q want=%q", limit, input, got, want)
			}
		}
	}
}

func TestTemplateTruncateKeepsInvalidLimitFailure(t *testing.T) {
	var previous string

	for _, fn := range []func(int, string) string{legacyTemplateTruncate, truncateTemplateText} {
		tmpl := texttemplate.Must(texttemplate.New("negative").Funcs(texttemplate.FuncMap{"truncate": fn}).Parse(`{{truncate -1 .}}`))

		var buf bytes.Buffer

		err := tmpl.Execute(&buf, "abc")
		if err == nil {
			t.Fatal("negative limit succeeded")
		}

		if previous != "" && err.Error() != previous {
			t.Errorf("negative limit failure changed: got=%q want=%q", err, previous)
		}

		previous = err.Error()
	}
}

func FuzzTemplateDisplayLineEquivalent(f *testing.F) {
	for _, s := range []string{"A\r\u200b\nB", "A\t\tB", " 한글 日本語 😀 ", "\xff\t\xfe"} {
		f.Add(s)
	}

	legacy := texttemplate.Must(texttemplate.New("legacy").Funcs(templateFuncs).Parse(legacyDisplayLineExpression))

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 8192 {
			t.Skip()
		}

		var buf bytes.Buffer

		if err := legacy.Execute(&buf, input); err != nil {
			t.Fatal(err)
		}

		if got := normalizeTemplateDisplayLine(input); got != buf.String() {
			t.Fatalf("displayline(%q)=%q want=%q", input, got, buf.String())
		}
	})
}

func FuzzTemplateTruncateEquivalent(f *testing.F) {
	for _, input := range []string{"", "plain", "한글 日本語 😀", "\xffa\xfe", strings.Repeat("한", 100)} {
		for _, limit := range []uint16{0, 3, 4, 64} {
			f.Add(input, limit)
		}
	}

	f.Fuzz(func(t *testing.T, input string, bound uint16) {
		if len(input) > 8192 {
			t.Skip()
		}

		limit := int(bound % 257)
		if got, want := truncateTemplateText(limit, input), legacyTemplateTruncate(limit, input); got != want {
			t.Fatalf("truncate(%d,%q)=%q want=%q", limit, input, got, want)
		}
	})
}
