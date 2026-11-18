package template

import "strings"

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
