package dispatchrun

import (
	"strings"
	"unicode/utf8"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/util"
)

func karingDisplayLine(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, util.KakaoZeroWidthSpace, "")), " ")
}

// 카드 표시값만 정리합니다. 원본 payload와 durable item 식별자는 변경하지 않습니다.
func karingDisplayTitle(value string) string {
	value = karingDisplayLine(value)
	if utf8.RuneCountInString(value) > 64 {
		return stringutil.TruncateString(value, 61)
	}

	return value
}
