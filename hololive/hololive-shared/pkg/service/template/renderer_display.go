package template

import (
	"strings"
	"unicode/utf8"
)

func normalizeTemplateDisplayLine(s string) string {
	s = strings.ReplaceAll(s, "\u200b", "")
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")

	return strings.TrimSpace(s)
}

func truncateTemplateText(maxLen int, s string) string {
	if len(s) <= maxLen {
		return s
	}

	if maxLen < 0 {
		return s[:maxLen]
	}

	keep := maxLen
	if keep > 3 {
		keep -= 3
	}

	count, end := 0, 0

	for index := range s {
		if count == keep {
			end = index
		}

		if count == maxLen {
			prefix := s[:end]
			if !utf8.ValidString(prefix) {
				prefix = string([]rune(prefix))
			}

			if maxLen > 3 {
				return prefix + "..."
			}

			return prefix
		}

		count++
	}

	return s
}
