package templateview

import "strings"

var memberNewsCategoryLabels = map[string]string{
	"birthday_live": "생일 라이브",
	"solo_live":     "솔로 라이브",
	"collab":        "콜라보",
	"event":         "이벤트",
	"goods":         "굿즈",
	"other":         "기타",
}

func MemberNewsCategoryLabel(raw string) string {
	if label, ok := memberNewsCategoryLabels[strings.TrimSpace(strings.ToLower(raw))]; ok {
		return label
	}
	return raw
}
