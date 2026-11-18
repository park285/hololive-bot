package kakaotext

import (
	"strings"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
)

func MarkdownLiteral(text string) string {
	if strings.ContainsRune(text, '\r') {
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
	}

	text = kakaoformat.EscapeMarkdown(strings.ReplaceAll(text, ZeroWidthSpace, ""))

	var b strings.Builder

	copied := 0

	for start := 0; start < len(text); {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += start
		}

		line := text[start:end]
		indent, columns := 0, 0

		for indent < len(line) && (line[indent] == ' ' || line[indent] == '\t') {
			if line[indent] == '\t' {
				columns += 4 - columns%4
			} else {
				columns++
			}

			indent++
		}

		if columns >= 4 && indent < len(line) {
			if copied == 0 {
				b.Grow(len(text) + len("&#32;"))
			}

			b.WriteString(text[copied:start])

			if text[start] == '\t' {
				b.WriteString("&#9;")
			} else {
				b.WriteString("&#32;")
			}

			copied = start + 1
		}

		start = end + 1
	}

	if copied == 0 {
		return text
	}

	b.WriteString(text[copied:])

	return b.String()
}
