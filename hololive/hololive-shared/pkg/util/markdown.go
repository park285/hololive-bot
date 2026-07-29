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

import "strings"

// 입력의 기존 ZWSP를 모두 제거한 뒤 자기 ZWSP만 삽입한다. 외부 유입 ZWSP를 남기면
// FoldForSeeMore의 연속-ZWSP 패딩 판정이 오작동하고, 결과가 ZWSP로 시작할 수 있어
// 조각을 이어붙일 때(`{{mdsafe .A}}{{mdsafe .B}}`) 경계에 연속 ZWSP가 생긴다.
func MarkdownNeutralize(s string) string {
	inserts, hasZeroWidth := markdownScan(s)
	if inserts == 0 && !hasZeroWidth {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + inserts*len(KakaoZeroWidthSpace))

	start := 0
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], KakaoZeroWidthSpace):
			b.WriteString(s[start:i])
			i += len(KakaoZeroWidthSpace)
			start = i
		case isMarkdownMarker(s[i]):
			b.WriteString(s[start : i+1])
			b.WriteString(KakaoZeroWidthSpace)
			i++
			start = i
		default:
			i++
		}
	}
	b.WriteString(s[start:])

	return b.String()
}

func markdownScan(s string) (inserts int, hasZeroWidth bool) {
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], KakaoZeroWidthSpace) {
			hasZeroWidth = true
			i += len(KakaoZeroWidthSpace)
			continue
		}
		if isMarkdownMarker(s[i]) {
			inserts++
		}
		i++
	}
	return inserts, hasZeroWidth
}

func isMarkdownMarker(c byte) bool {
	switch c {
	case '*', '_', '`', '~', ']', '#':
		return true
	default:
		return false
	}
}
