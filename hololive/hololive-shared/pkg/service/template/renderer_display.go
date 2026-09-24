package template

import (
	"strings"
	"unicode/utf8"
)

// 기존 기본 본문의 치환 순서와 공백 수를 보존합니다. ZWSP 제거 뒤 CRLF가 합쳐질 수 있습니다.
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
		// 음수 한도는 기존과 같이 경계 오류를 내며 template.Execute가 이를 에러로 돌려줍니다.
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
				// 잘린 부분의 잘못된 UTF-8은 기존 []rune 변환과 동일하게 복원합니다.
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
