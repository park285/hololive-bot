package util

import (
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// 카카오 메시지 관련 상수 목록.
const (
	// KakaoSeeMorePadding: 카카오톡 '전체 보기' 기능을 위한 패딩 길이
	KakaoSeeMorePadding = 500
	KakaoZeroWidthSpace = "\u200b"
)

// ApplyKakaoSeeMorePadding: 텍스트에 투명 공백(Zero Width Space)을 추가하여 카카오톡에서 '전체 보기'로 접히도록 만든다.
func ApplyKakaoSeeMorePadding(text, instruction string) string {
	if stringutil.TrimSpace(text) == "" {
		return text
	}

	message := stringutil.TrimSpace(instruction)

	var builder strings.Builder
	builder.Grow(len(text) + KakaoSeeMorePadding + len(message) + 2)

	if message != "" {
		builder.WriteString(message)
	}
	builder.WriteString(strings.Repeat(KakaoZeroWidthSpace, KakaoSeeMorePadding))
	if !strings.HasPrefix(text, "\n") {
		builder.WriteByte('\n')
	}
	builder.WriteString(text)

	return builder.String()
}
