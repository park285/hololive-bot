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
	"testing"

	"github.com/kapu/hololive-shared/pkg/util"
)

func TestFoldForSeeMore(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat(util.KakaoZeroWidthSpace, KakaoSeeMorePadding)
	longRest := strings.Repeat("가", 300)
	multi := "헤더 라인\n" + longRest

	folded := FoldForSeeMore(multi)
	if folded != "헤더 라인"+padding+"\n"+longRest {
		t.Fatalf("first line fold changed: %q", folded[:30])
	}

	if again := FoldForSeeMore(folded); again != folded {
		t.Error("fold is not idempotent")
	}

	short := "짧은 메시지\n본문"
	if got := FoldForSeeMore(short); got == short {
		t.Errorf("짧은 여러 줄 본문이 접히지 않음: %q", got)
	}

	single := strings.Repeat("a", 300)
	if got := FoldForSeeMore(single); got != single {
		t.Error("한 줄 입력이 변형됨")
	}

	blankRest := strings.Repeat("가", 300) + "\n   "
	if got := FoldForSeeMore(blankRest); got != blankRest {
		t.Error("공백 본문 입력이 변형됨")
	}
}

func TestFoldForSeeMoreKeepsHeadParagraph(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat(util.KakaoZeroWidthSpace, KakaoSeeMorePadding)
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

			got := FoldForSeeMore(text)
			if want := tt.head + padding + tt.sep + body; got != want {
				t.Fatalf("head paragraph not kept: %q", got[:min(len(got), 80)])
			}

			if strings.ReplaceAll(got, util.KakaoZeroWidthSpace, "") != text {
				t.Error("visible text changed")
			}
		})
	}
}

func TestFoldForSeeMoreFallsBackToFirstLineForLongHead(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat(util.KakaoZeroWidthSpace, KakaoSeeMorePadding)
	lines := make([]string, seeMoreHeadMaxLines+1)

	for i := range lines {
		lines[i] = "머리 줄"
	}

	rest := strings.Join(lines[1:], "\n") + "\n\n" + strings.Repeat("본문 ", 120)
	text := lines[0] + "\n" + rest

	if got := FoldForSeeMore(text); got != lines[0]+padding+"\n"+rest {
		t.Fatalf("head over limit must fold after first line: %q", got[:min(len(got), 80)])
	}
}

func TestMarkdownNeutralize_StrippedBodyStillFolds(t *testing.T) {
	t.Parallel()

	title := "라이브" + strings.Repeat(util.KakaoZeroWidthSpace, 4) + "제목"
	body := title + "\n" + strings.Repeat("가나다라마바사아자차카타파하", 25)

	folded := FoldForSeeMore(util.MarkdownNeutralize(body))
	if !strings.Contains(folded, strings.Repeat(util.KakaoZeroWidthSpace, KakaoSeeMorePadding)) {
		t.Errorf("외부 유입 ZWSP run 때문에 fold가 억제됨: %q", folded[:60])
	}
}

func TestFoldSurvivesNeutralizedBody(t *testing.T) {
	t.Parallel()

	body := "**헤더 라인**\n" + strings.Repeat("가~나*다_라#마]바`사", 40)
	neutralized := util.MarkdownNeutralize(body)

	if !strings.Contains(neutralized, util.KakaoZeroWidthSpace) {
		t.Fatal("전제 실패: neutralize 결과에 ZWSP가 없음")
	}

	folded := FoldForSeeMore(neutralized)
	if folded == neutralized {
		t.Fatal("neutralize된 본문이 fold되지 않음")
	}

	if !strings.Contains(folded, strings.Repeat(util.KakaoZeroWidthSpace, KakaoSeeMorePadding)) {
		t.Errorf("ZWSP %d-run 패딩이 삽입되지 않음", KakaoSeeMorePadding)
	}

	head := "*" + util.KakaoZeroWidthSpace + "*" + util.KakaoZeroWidthSpace + "헤더 라인"
	if !strings.HasPrefix(folded, head) {
		t.Error("첫 줄이 보존되지 않음")
	}

	if again := FoldForSeeMore(folded); again != folded {
		t.Error("neutralize된 본문에서 fold가 멱등하지 않음")
	}
}

func TestShouldFoldItems(t *testing.T) {
	for _, tc := range []struct {
		count int
		want  bool
	}{{0, false}, {1, false}, {2, true}, {100, true}} {
		if got := ShouldFoldItems(tc.count); got != tc.want {
			t.Errorf("count=%d: got %t, want %t", tc.count, got, tc.want)
		}
	}
}

func TestFoldForSeeMorePreservesEmptyAndExistingPadding(t *testing.T) {
	for _, body := range []string{"", " \n\t", "한 줄", "헤더" + strings.Repeat(util.KakaoZeroWidthSpace, 500) + "\n본문", "헤더" + strings.Repeat(util.KakaoZeroWidthSpace, 2) + "\n본문"} {
		if got := FoldForSeeMore(body); got != body {
			t.Errorf("existing body changed: %q", got)
		}
	}
}
