package util

import (
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// NormalizeSuffix: 문자열에서 "짱", "쨩"과 같은 한국어 호칭 접미사를 제거하고 정규화합니다.
// hololive-kakao-bot-go 전용 함수입니다.
func NormalizeSuffix(s string) string {
	normalized := stringutil.Normalize(s)

	if strings.HasSuffix(normalized, "짱") {
		return normalized[:len(normalized)-len("짱")]
	}

	if strings.HasSuffix(normalized, "쨩") {
		return normalized[:len(normalized)-len("쨩")]
	}

	return normalized
}
