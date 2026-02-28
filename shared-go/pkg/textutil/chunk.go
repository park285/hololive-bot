package textutil

import (
	"strings"
	"unicode/utf8"
)

// KakaoMessageMaxLength: 카카오톡 메시지 최대 길이 (문자 수 기준)
const KakaoMessageMaxLength = 500

// ChunkByLines: 입력된 문자열을 최대 길이에 맞춰 라인 단위로 분할합니다.
func ChunkByLines(input string, maxLength int) []string {
	if maxLength <= 0 {
		return []string{input}
	}

	lines := strings.Split(input, "\n")
	chunks := make([]string, 0, len(lines))

	var current strings.Builder
	currentLength := 0

	flush := func() {
		if currentLength == 0 {
			return
		}
		chunks = append(chunks, current.String())
		current.Reset()
		currentLength = 0
	}

	for _, raw := range lines {
		// 한 라인이 maxLength보다 긴 경우 강제로 분할
		var subLines []string
		if utf8.RuneCountInString(raw) > maxLength {
			runes := []rune(raw)
			for len(runes) > 0 {
				if len(runes) <= maxLength {
					subLines = append(subLines, string(runes))
					break
				}
				subLines = append(subLines, string(runes[:maxLength]))
				runes = runes[maxLength:]
			}
		} else {
			subLines = []string{raw}
		}

		for _, line := range subLines {
			separator := 0
			if currentLength > 0 {
				separator = 1
			}

			lineLength := utf8.RuneCountInString(line)
			if currentLength+separator+lineLength <= maxLength {
				if separator == 1 {
					current.WriteByte('\n')
				}
				current.WriteString(line)
				currentLength += separator + lineLength
				continue
			}

			flush()
			current.WriteString(line)
			currentLength = lineLength
		}
	}

	flush()
	return chunks
}
