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
	"testing"
)

func TestMarkdownNeutralize(t *testing.T) {
	t.Parallel()

	t.Run("passthrough", func(t *testing.T) {
		t.Parallel()

		for _, in := range []string{"", "마커 없는 평문", "hello world 123", "가격은 1,000원 (부가세 별도)"} {
			if got := MarkdownNeutralize(in); got != in {
				t.Errorf("MarkdownNeutralize(%q) = %q, want unchanged", in, got)
			}
		}
	})

	t.Run("markers broken", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name    string
			in      string
			notWant string
		}{
			{"bold", "**굵게**", "**"},
			{"italic", "__이탤릭__", "__"},
			{"code", "```code```", "``"},
			{"strike", "~~취소선~~", "~~"},
			{"link", "[제목](https://example.com)", "]("},
			{"heading", "# 제목", "# "},
			{"entity", "Fish &amp; Chips", "&amp;"},
			{"autolink", "<someone@example.invalid>", "<someone"},
			{"escape", `a\\b`, `\\`},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got := MarkdownNeutralize(tc.in)
				if strings.Contains(got, tc.notWant) {
					t.Errorf("MarkdownNeutralize(%q) = %q, still contains %q", tc.in, got, tc.notWant)
				}
			})
		}
	})

	t.Run("zwsp follows every marker", func(t *testing.T) {
		t.Parallel()

		got := MarkdownNeutralize("a*b_c`d~e]f#g")
		want := "a*" + KakaoZeroWidthSpace +
			"b_" + KakaoZeroWidthSpace +
			"c`" + KakaoZeroWidthSpace +
			"d~" + KakaoZeroWidthSpace +
			"e]" + KakaoZeroWidthSpace +
			"f#" + KakaoZeroWidthSpace + "g"

		if got != want {
			t.Errorf("MarkdownNeutralize = %q, want %q", got, want)
		}
	})
}

func TestMarkdownNeutralize_PreservesVisibleRunes(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"**굵게** _이탤릭_ `code` ~~취소선~~ [제목](https://example.com) # 헤딩",
		"*_`~]#",
		"별표*중간_밑줄",
		"이모지 🎤 와 마커 **혼합**",
	}

	for _, in := range inputs {
		got := MarkdownNeutralize(in)
		if stripped := strings.ReplaceAll(got, KakaoZeroWidthSpace, ""); stripped != in {
			t.Errorf("visible text changed: got %q, want %q", stripped, in)
		}
	}
}

func TestMarkdownNeutralize_NoConsecutiveZeroWidth(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"***",
		"**굵게** ~~취소선~~",
		"]]]###",
		"마커 뒤 ZWSP 이미 존재: *" + KakaoZeroWidthSpace + "텍스트",
	}

	for _, in := range inputs {
		got := MarkdownNeutralize(in)
		if strings.Contains(got, KakaoZeroWidthSpace+KakaoZeroWidthSpace) {
			t.Errorf("MarkdownNeutralize(%q) produced consecutive ZWSP: %q", in, got)
		}
	}
}

func TestMarkdownNeutralize_Idempotent(t *testing.T) {
	t.Parallel()

	in := "**굵게** [제목](https://example.com) ~~취소선~~ # 헤딩"
	once := MarkdownNeutralize(in)

	if twice := MarkdownNeutralize(once); twice != once {
		t.Errorf("MarkdownNeutralize is not idempotent: %q != %q", twice, once)
	}
}

func TestMarkdownNeutralize_StripsIncomingZeroWidth(t *testing.T) {
	t.Parallel()

	run2 := KakaoZeroWidthSpace + KakaoZeroWidthSpace

	inputs := []string{
		"제목" + run2 + "스타일링",
		run2 + "선행 런",
		"마커 없는 본문" + run2,
		"**굵게**" + run2 + "[제목](https://example.com)",
		strings.Repeat(KakaoZeroWidthSpace, 500),
	}

	for _, in := range inputs {
		got := MarkdownNeutralize(in)
		if strings.Contains(got, run2) {
			t.Errorf("MarkdownNeutralize(%q) 통과 후에도 연속 ZWSP 잔존: %q", in, got)
		}
	}
}

func TestMarkdownNeutralize_ConcatenationClosed(t *testing.T) {
	t.Parallel()

	fields := []string{
		"끝이 마커*",
		KakaoZeroWidthSpace + "선행 ZWSP로 시작",
		"**굵게**",
		"평범한 제목",
		"",
		"#",
		"끝이 ZWSP" + KakaoZeroWidthSpace,
	}

	outs := make([]string, 0, len(fields))
	for _, f := range fields {
		outs = append(outs, MarkdownNeutralize(f))
	}

	for i, a := range outs {
		for j, b := range outs {
			if strings.Contains(a+b, KakaoZeroWidthSpace+KakaoZeroWidthSpace) {
				t.Errorf("연결 [%d]+[%d]에서 연속 ZWSP 발생: %q + %q", i, j, a, b)
			}
		}
	}
}
