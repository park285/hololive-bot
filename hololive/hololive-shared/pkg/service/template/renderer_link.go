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

// 앞의 7개가 YouTube 계열이다. YouTube+치지직 동시 송출만 " | "로 묶인 두 링크를 허용한다.
const youtubeLinkPrefixCount = 7

const chzzkLivePrefix = "https://chzzk.naver.com/live/"

// YouTube와 치지직 링크를 " | "로 묶은 동시 송출 값은 두 줄로 나눕니다. 빈 결과를 받은 템플릿은 값을 mdsafe로 보호해 일반 문자로 표시합니다. 결과는 입력보다 길어지지 않습니다.
func trustedLink(url string) string {
	if url == "" || strings.ContainsAny(url, "\t\n\r()[]<>\\") {
		return ""
	}

	if first, second, ok := strings.Cut(url, " | "); ok {
		if !strings.Contains(second, " | ") && hasAnyPrefix(url, trustedLinkPrefixes[:youtubeLinkPrefixCount]) &&
			strings.HasPrefix(second, chzzkLivePrefix) && !strings.Contains(first, " ") && !strings.Contains(second, " ") {
			return first + "\n" + second
		}

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
