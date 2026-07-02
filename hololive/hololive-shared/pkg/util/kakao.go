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
	// KakaoSeeMorePadding: 카카오톡 '전체 보기' 기능을 위한 패딩 길이
	KakaoSeeMorePadding   = 500
	KakaoSeeMoreThreshold = 250
	KakaoZeroWidthSpace   = "\u200b"
)

// FoldForSeeMore는 첫 줄 뒤에 zero-width space 패딩을 삽입해 KakaoTalk이
// 본문을 '전체보기'로 접게 만든다. 임계 이하·한 줄짜리·이미 패딩된 텍스트는
// 그대로 반환한다(멱등).
func FoldForSeeMore(text string, threshold int) string {
	if threshold <= 0 || utf8.RuneCountInString(text) <= threshold {
		return text
	}
	if strings.Contains(text, KakaoZeroWidthSpace) {
		return text
	}

	head, rest, found := strings.Cut(text, "\n")
	if !found || strings.TrimSpace(rest) == "" {
		return text
	}

	return head + "\n" + strings.Repeat(KakaoZeroWidthSpace, KakaoSeeMorePadding) + "\n" + rest
}
