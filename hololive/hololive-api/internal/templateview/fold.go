package templateview

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/kakaotext"
)

const (
	KakaoSeeMorePadding = 500
	seeMoreHeadMaxLines = 4
)

func ShouldFoldItems(displayCount int) bool { return displayCount >= 2 }

func FoldForSeeMore(text string) string {
	if strings.Contains(text, kakaotext.ZeroWidthSpace+kakaotext.ZeroWidthSpace) {
		return text
	}

	head, rest, found := cutSeeMoreHead(text)
	if !found || strings.TrimSpace(rest) == "" {
		return text
	}

	return head + strings.Repeat(kakaotext.ZeroWidthSpace, KakaoSeeMorePadding) + "\n" + rest
}

func cutSeeMoreHead(text string) (head, rest string, found bool) {
	firstLine, afterFirst, found := strings.Cut(text, "\n")
	if !found {
		return text, "", false
	}

	headLen := len(firstLine)
	next := afterFirst

	for range seeMoreHeadMaxLines {
		line, remaining, more := strings.Cut(next, "\n")
		if strings.TrimSpace(line) == "" {
			return text[:headLen], next, true
		}

		if !more {
			break
		}

		headLen += 1 + len(line)

		next = remaining
	}

	return firstLine, afterFirst, true
}
