package matcher

import (
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"
)

// NormalizeSuffix는 멤버 이름 검색어를 정규화하고 애칭 접미사 "짱"·"쨩"을 떼어 낸다.
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
