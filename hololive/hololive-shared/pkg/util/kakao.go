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

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// 카카오 메시지 관련 상수 목록.
const (
	// KakaoSeeMorePadding: 카카오톡 '전체 보기' 기능을 위한 패딩 길이
	KakaoSeeMorePadding = 500
	KakaoZeroWidthSpace = "\u200b"
)

func ApplyKakaoSeeMorePadding(text, instruction string) string {
	if stringutil.TrimSpace(text) == "" {
		return text
	}

	message := stringutil.TrimSpace(instruction)

	var builder strings.Builder
	builder.Grow(len(text) + KakaoSeeMorePadding + len(message) + 2)

	if message != "" {
		builder.WriteString(message)
	}
	builder.WriteString(strings.Repeat(KakaoZeroWidthSpace, KakaoSeeMorePadding))
	if !strings.HasPrefix(text, "\n") {
		builder.WriteByte('\n')
	}
	builder.WriteString(text)

	return builder.String()
}
