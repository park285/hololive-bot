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

// 외부 유입 ZWSP를 남기면 FoldForSeeMore의 연속-ZWSP 패딩 판정이 오작동하고, 결과가 ZWSP로
// 시작할 수 있어 조각 연결(`{{mdsafe .A}}{{mdsafe .B}}`) 경계에 연속 ZWSP가 생기므로 전부 제거한다.
// 같은 이유로 FoldForSeeMore를 거친 텍스트에 적용하면 패딩이 삭제되어 접기가 풀린다 — fold 이전 필드 단위로만 쓴다.
func MarkdownNeutralize(s string) string {
	inserts, hasZeroWidth := markdownScan(s)
	if inserts == 0 && !hasZeroWidth {
		return s
	}

	if hasZeroWidth {
		s = strings.ReplaceAll(s, KakaoZeroWidthSpace, "")
	}

	return insertZeroWidthAfterMarkers(s, inserts)
}

func insertZeroWidthAfterMarkers(s string, inserts int) string {
	if inserts == 0 {
		return s
	}

	var b strings.Builder

	b.Grow(len(s) + inserts*len(KakaoZeroWidthSpace))

	start := 0

	for i := range len(s) {
		if isMarkdownMarker(s[i]) {
			b.WriteString(s[start : i+1])
			b.WriteString(KakaoZeroWidthSpace)

			start = i + 1
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
