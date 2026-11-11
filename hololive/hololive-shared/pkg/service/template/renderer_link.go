package template

import "strings"

// 알림 템플릿이 링크로 그대로 내보내는 출처다. 이 목록 밖의 URL은 링크가 아니라 보호된 일반 문자로 표시한다.
var trustedLinkPrefixes = []string{
	"https://short.holoshi.com/l/",
	"https://www.youtube.com/watch?",
	"https://youtube.com/watch?",
	"https://m.youtube.com/watch?",
	"https://www.youtube.com/live/",
	"https://youtube.com/live/",
	"https://youtu.be/",
	"https://www.twitch.tv/",
	"https://twitch.tv/",
	"https://chzzk.naver.com/live/",
}

// trustedLink는 알림 URL이 신뢰 출처의 링크이면 그 URL을, 아니면 빈 문자열을 돌려줍니다. 빈 결과를 받은 템플릿은 값을
// mdsafe로 보호해 일반 문자로 표시합니다. 결과는 입력보다 길어지지 않습니다.
func trustedLink(url string) string {
	if url == "" || strings.ContainsAny(url, "\t\n\r()[]<>\\") {
		return ""
	}

	if strings.ContainsAny(url, " |") || !hasAnyPrefix(url, trustedLinkPrefixes) {
		return ""
	}

	return url
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}

	return false
}
