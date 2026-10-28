package kakaotext

import (
	"strings"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
)

// MarkdownLiteral은 평문 필드의 가시 문자와 URL을 보존하며 Markdown 해석을 막는다.
// 요약·캐시는 평문으로 유지하고 표시 경계에서 한 번만 호출한다. 외부 ZWSP는 접기 패딩으로
// 오인되지 않게 제거하므로, FoldForSeeMore 이후나 정적 서식이 포함된 메시지에는 적용하지 않는다.
func MarkdownLiteral(text string) string {
	// 두 소비 파서와 같은 줄 경계를 사용해야 단독 CR 뒤의 목록·코드 들여쓰기도 보호된다.
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

		indent, columns := start, 0
		for indent < end && (text[indent] == ' ' || text[indent] == '\t') {
			if text[indent] == '\t' {
				columns += 4 - columns%4
			} else {
				columns++
			}
			indent++
		}

		if columns >= 4 && indent < end {
			if copied == 0 {
				b.Grow(len(text) + len("&#32;"))
			}
			b.WriteString(text[copied:start])
			// 첫 공백을 문자 참조로 쓰면 코드블록이 되지 않고, 렌더 후에는 원래 공백/탭으로 복원된다.
			// 숨은 문자를 추가하지 않으므로 명령·URL 복사와 기존 접기 패딩 판정도 유지한다.
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
