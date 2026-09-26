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

func TestFoldForSeeMore(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat(KakaoZeroWidthSpace, KakaoSeeMorePadding)
	longRest := strings.Repeat("가", 300)
	multi := "헤더 라인\n" + longRest

	folded := FoldForSeeMore(multi, KakaoSeeMoreThreshold)
	if folded != "헤더 라인"+padding+"\n"+longRest {
		t.Fatalf("first line fold changed: %q", folded[:30])
	}

	if again := FoldForSeeMore(folded, KakaoSeeMoreThreshold); again != folded {
		t.Error("fold is not idempotent")
	}

	short := "짧은 메시지\n본문"
	if got := FoldForSeeMore(short, KakaoSeeMoreThreshold); got != short {
		t.Errorf("threshold 이하 입력이 변형됨: %q", got)
	}

	single := strings.Repeat("a", 300)
	if got := FoldForSeeMore(single, KakaoSeeMoreThreshold); got != single {
		t.Error("한 줄 입력이 변형됨")
	}

	if got := FoldForSeeMore(multi, 0); got != multi {
		t.Error("threshold<=0 입력이 변형됨")
	}

	blankRest := strings.Repeat("가", 300) + "\n   "
	if got := FoldForSeeMore(blankRest, KakaoSeeMoreThreshold); got != blankRest {
		t.Error("공백 본문 입력이 변형됨")
	}
}

func TestFoldForSeeMoreKeepsHeadParagraph(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat(KakaoZeroWidthSpace, KakaoSeeMorePadding)
	body := "1 · 채널\n" + strings.Repeat("긴 제목 ", 60)

	tests := []struct {
		name string
		head string
		sep  string
	}{
		{name: "title only", head: "🔔 설정된 알람 · 16개", sep: "\n\n"},
		{name: "count on second line", head: "📅 채널 일정\n7일 이내 · 5개", sep: "\n\n"},
		{name: "max lines", head: "방송 이력 3건\n멤버: 미코\n타입: 노래\n일부 결과만 표시했습니다.", sep: "\n\n"},
		{name: "space-only blank line", head: "📅 예정 방송 · 3개\n24시간 이내", sep: "\n  \n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text := tt.head + tt.sep + body

			got := FoldForSeeMore(text, KakaoSeeMoreThreshold)
			if want := tt.head + padding + tt.sep + body; got != want {
				t.Fatalf("head paragraph not kept: %q", got[:min(len(got), 80)])
			}

			if strings.ReplaceAll(got, KakaoZeroWidthSpace, "") != text {
				t.Error("visible text changed")
			}
		})
	}
}

func TestFoldForSeeMoreFallsBackToFirstLineForLongHead(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat(KakaoZeroWidthSpace, KakaoSeeMorePadding)
	lines := make([]string, KakaoSeeMoreHeadMaxLines+1)

	for i := range lines {
		lines[i] = "머리 줄"
	}

	rest := strings.Join(lines[1:], "\n") + "\n\n" + strings.Repeat("본문 ", 120)
	text := lines[0] + "\n" + rest

	if got := FoldForSeeMore(text, KakaoSeeMoreThreshold); got != lines[0]+padding+"\n"+rest {
		t.Fatalf("head over limit must fold after first line: %q", got[:min(len(got), 80)])
	}
}
