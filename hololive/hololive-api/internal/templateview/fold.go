// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package templateview

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/util"
)

const (
	// KakaoSeeMorePadding은 카카오톡 전체보기 패딩 길이다.
	KakaoSeeMorePadding = 500
	// 접힌 화면에 남기는 머리 문단의 최대 줄 수다.
	seeMoreHeadMaxLines = 4
)

// ShouldFoldItems는 실제 표시 항목이 두 개 이상인 목록과 보고서만 접는다.
func ShouldFoldItems(displayCount int) bool { return displayCount >= 2 }

// FoldForSeeMore는 머리 문단 끝에 zero-width space 패딩을 붙여 KakaoTalk이
// 머리 문단과 '전체보기'만 보이도록 접게 만든다. 머리 문단은 첫 빈 줄 앞의 줄이며,
// seeMoreHeadMaxLines 안에 빈 줄이 없으면 첫 줄만 남긴다.
// 빈 본문·한 줄짜리·이미 패딩된 텍스트는 그대로 반환한다(멱등).
func FoldForSeeMore(text string) string {
	// 단발 ZWSP는 MarkdownNeutralize가 남긴 것이므로, 패딩 판정은 연속 2개 이상으로만 한다.
	if strings.Contains(text, util.KakaoZeroWidthSpace+util.KakaoZeroWidthSpace) {
		return text
	}

	head, rest, found := cutSeeMoreHead(text)
	if !found || strings.TrimSpace(rest) == "" {
		return text
	}

	// 패딩을 머리 문단 마지막 줄에 붙여야 접힌 화면에 빈 줄이 따로 생기지 않는다.
	return head + strings.Repeat(util.KakaoZeroWidthSpace, KakaoSeeMorePadding) + "\n" + rest
}

// 개수·기간·표시 한도 안내가 제목 다음 줄에 있어도 접힌 화면에 남도록 머리 문단 단위로 자른다.
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
