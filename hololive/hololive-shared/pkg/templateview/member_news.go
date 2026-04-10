package templateview

import "strings"

func MemberNewsCategoryLabel(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "birthday_live":
		return "생일 라이브"
	case "solo_live":
		return "솔로 라이브"
	case "collab":
		return "콜라보"
	case "event":
		return "이벤트"
	case "goods":
		return "굿즈"
	case "other":
		return "기타"
	default:
		return raw
	}
}
