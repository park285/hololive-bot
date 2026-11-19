package matcher

import (
	"strings"

	"github.com/park285/shared-go/core/stringutil"
)

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
