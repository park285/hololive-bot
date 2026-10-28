package kakaotext

import (
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
)

func TestMarkdownLiteralPreservesVisibleFields(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"공개 ||가격|| **공식명** _멤버_ ~~안내~~ `원문` &amp; <tag>",
		"# 제목\n> 인용\n- 안내\n1. 순서\n+ 추가\n===",
		"원제\r\n- 무료 **안내**\r\n다음 줄",
		"https://example.org/a_b*(c)?q=||x||&amp;v=~y~#z",
		"주소 https://example.org/a_b?x=1&y=2 뒤의 ||문자||",
		"!썸네일 a_b-c\\d",
		"😀 日本語 **이름**" + strings.Repeat(ZeroWidthSpace, 500),
		"안내\n\n    **원제**\n\thttps://example.org/a_b?x=1&y=2",
		"안내\r\n\r\n \t||가격|| **원제**\r\n  \thttps://example.org/a_b?x=1&y=2\r\n\r\n끝",
		"안내\r\r    **원제**\r\thttps://example.org/a_b?x=1&y=2",
		"안내\r- 항목\r# 제목\r> 인용\r1. 순서",
	} {
		t.Run(input, func(t *testing.T) {
			want := strings.NewReplacer(ZeroWidthSpace, "", "\r\n", "\n", "\r", "\n").Replace(input)
			literal := MarkdownLiteral(input)
			if strings.Contains(literal, ZeroWidthSpace+ZeroWidthSpace) {
				t.Fatal("외부 문자가 접기 패딩으로 남음")
			}
			if got := kakaoformat.Render(literal); got != want {
				t.Fatalf("평문 필드 변형: got %q, want %q", got, want)
			}
		})
	}
}

func TestMarkdownLiteralPreservesMarkersInsideStaticBold(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"||멤버|| 끝**", "**제목** _원문_", "끝\\", "마커" + ZeroWidthSpace + "**"} {
		visible := kakaoformat.Render("**" + MarkdownLiteral(field) + "**")
		if want := strings.ReplaceAll(field, ZeroWidthSpace, ""); !strings.Contains(visible, want) {
			t.Fatalf("정적 강조 안에서 동적 기호 손실: %q in %q", want, visible)
		}
	}
}

func TestMarkdownLiteralIndentedBlankLinesKeepContent(t *testing.T) {
	t.Parallel()

	const url = "https://example.org/a_b?x=1&y=2"
	visible := kakaoformat.Render(MarkdownLiteral("안내\r\n    \r \t\r\n    **원제**\r\t" + url + "\n\n끝"))
	for _, field := range []string{"안내", "    **원제**", "\t" + url, "끝"} {
		if !strings.Contains(visible, field) {
			t.Fatalf("빈 들여쓰기 줄 뒤의 본문 변형: %q in %q", field, visible)
		}
	}
	if strings.Contains(visible, "\\*") || strings.Contains(visible, "\\_") || strings.Contains(visible, "\\&") {
		t.Fatalf("코드블록 안에 표시용 이스케이프 잔존: %q", visible)
	}
}
