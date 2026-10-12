package celebration

import (
	"slices"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// 생일 당일 본인 채널이라도 최초공개 VLOG·스폰서 방송처럼 생일과 무관한 와꾸가 있고,
// 이들이 멤버·일자 상한을 먼저 차지하면 실제 생일 방송 알림이 막힌다. 그래서 제목에
// 생일 키워드가 있는 와꾸만 생일 방송으로 본다. 키워드가 없는 생일 방송은 놓친다.
// 키워드는 NFKC 정규화·소문자화·공백 축약한 제목의 부분 문자열로 비교해 전각·반각 표기도 잡는다.
var birthdayStreamTitleKeywords = []string{
	"birthday", "bday", "b-day",
	"誕生日", "誕生祭", "誕生会", "生誕", "たんじょうび", "バースデー", "バースデイ", "バースディ",
	"생일",
	"ulang tahun", "ultah",
}

func isBirthdayStreamTitle(title string) bool {
	normalized := strings.Join(strings.Fields(strings.ToLower(norm.NFKC.String(title))), " ")

	return slices.ContainsFunc(birthdayStreamTitleKeywords, func(keyword string) bool {
		return strings.Contains(normalized, keyword)
	})
}
