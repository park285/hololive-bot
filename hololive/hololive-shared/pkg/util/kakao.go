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

package util

import (
	"strings"
	"unicode/utf8"
)

// 카카오 메시지 관련 상수 목록.
const (
	// KakaoSeeMorePadding: 카카오톡 '전체 보기' 기능을 위한 패딩 길이.
	KakaoSeeMorePadding   = 500
	KakaoSeeMoreThreshold = 250
	// KakaoSeeMoreHeadMaxLines: 접힌 화면에 남기는 머리 문단의 최대 줄 수.
	KakaoSeeMoreHeadMaxLines = 4
	KakaoZeroWidthSpace      = "\u200b"
)

// FoldForSeeMore는 머리 문단 끝에 zero-width space 패딩을 붙여 KakaoTalk이
// 머리 문단과 '전체보기'만 보이도록 접게 만든다. 머리 문단은 첫 빈 줄 앞의 줄이며,
// KakaoSeeMoreHeadMaxLines 안에 빈 줄이 없으면 첫 줄만 남긴다.
// 임계 이하·한 줄짜리·이미 패딩된 텍스트는 그대로 반환한다(멱등).
func FoldForSeeMore(text string, threshold int) string {
	if threshold <= 0 || utf8.RuneCountInString(text) <= threshold {
		return text
	}

	// 단발 ZWSP는 MarkdownNeutralize가 남긴 것이므로, 패딩 판정은 연속 2개 이상으로만 한다.
	if strings.Contains(text, KakaoZeroWidthSpace+KakaoZeroWidthSpace) {
		return text
	}

	head, rest, found := cutSeeMoreHead(text)
	if !found || strings.TrimSpace(rest) == "" {
		return text
	}

	// 패딩을 머리 문단 마지막 줄에 붙여야 접힌 화면에 빈 줄이 따로 생기지 않는다.
	return head + strings.Repeat(KakaoZeroWidthSpace, KakaoSeeMorePadding) + "\n" + rest
}

// 개수·기간·표시 한도 안내가 제목 다음 줄에 있어도 접힌 화면에 남도록 머리 문단 단위로 자른다.
func cutSeeMoreHead(text string) (head, rest string, found bool) {
	firstEnd := strings.IndexByte(text, '\n')
	if firstEnd < 0 {
		return text, "", false
	}

	end := firstEnd

	for range KakaoSeeMoreHeadMaxLines {
		next := text[end+1:]

		line, _, _ := strings.Cut(next, "\n")
		if strings.TrimSpace(line) == "" {
			return text[:end], next, true
		}

		lineEnd := strings.IndexByte(next, '\n')
		if lineEnd < 0 {
			break
		}

		end += 1 + lineEnd
	}

	return text[:firstEnd], text[firstEnd+1:], true
}
