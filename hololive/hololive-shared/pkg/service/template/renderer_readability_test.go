package template

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
)

func TestSeedTemplates_KakaoReadabilitySamples(t *testing.T) {
	pool := dbtest.NewPool(t)

	for _, key := range sampledata.GetAllTemplateKeys() {
		t.Run(string(key), func(t *testing.T) {
			data := sampledata.GetTemplateSampleData(key)
			body := seedBody(t, pool, key)
			rendered := renderSeedBody(t, key, body, data)
			out := kakaoformat.Render(rendered)

			for line := range strings.SplitSeq(out, "\n") {
				if strings.Contains(line, "https://") && !strings.HasPrefix(line, "https://") {
					t.Errorf("URL이 독립된 줄이 아님: %q", line)
				}
			}

			if strings.Contains(rendered, "](") || !utf8.ValidString(out) || strings.TrimSpace(out) == "" {
				t.Errorf("숨은 링크 또는 유효하지 않은 일반 텍스트 출력: %q", out)
			}

			t.Logf("KAKAO_PREVIEW %s\n%s\nEND_PREVIEW", key, out)
		})
	}
}

func TestSeedTemplates_KakaoLiveLongTitlesAndMissingFields(t *testing.T) {
	pool := dbtest.NewPool(t)
	body := seedBody(t, pool, domain.TemplateKeyCmdLiveStreams)

	const url = "https://youtu.be/a_b#c"

	for _, title := range []string{
		strings.Repeat("긴 한글 제목 ", 20),
		strings.Repeat("長い日本語の配信タイトル", 10),
		strings.Repeat("A long English stream title ", 10),
		"**강조**_[테스트] ~표시~ #제목\r\n다음 줄",
		"",
	} {
		t.Run(fmt.Sprintf("title_%d_%s", utf8.RuneCountInString(title), string([]rune(title + " ")[:1])), func(t *testing.T) {
			rendered := renderSeedBody(t, domain.TemplateKeyCmdLiveStreams, body, map[string]any{
				fieldCount: 3,
				fieldStreams: []map[string]any{
					{fieldChannelName: "Kureiji Ollie Ch. hololive", fieldTitle: title, fieldURL: url, "ViewerCount": 987654, fieldElapsed: ""},
					{fieldChannelName: "두 번째 채널", fieldTitle: "제목만 있음", fieldURL: "", "ViewerCount": 0, fieldElapsed: ""},
					{fieldChannelName: "세 번째 채널", fieldTitle: "", fieldURL: "", "ViewerCount": 0, fieldElapsed: ""},
				},
			})
			out := kakaoformat.Render(rendered)

			for _, required := range []string{"Kureiji Ollie Ch. hololive", "두 번째 채널", "세 번째 채널", "제목만 있음"} {
				require.Contains(t, rendered, required, "입력 항목 소실")
			}

			if !hasSeedLine(out, url) {
				t.Errorf("독립 URL 소실: %q", out)
			}

			if strings.Contains(rendered, "987654") || !utf8.ValidString(out) {
				t.Errorf("시청자 수 노출 또는 UTF-8 손상: %q", out)
			}

			for line := range strings.SplitSeq(rendered, "\n") {
				line = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(line, kakaotext.ZeroWidthSpace), "**"), "**")

				if strings.HasPrefix(line, "긴 한글") || strings.HasPrefix(line, "長い日本語") || strings.HasPrefix(line, "A long") {
					if utf8.RuneCountInString(line) > 64 || !strings.HasSuffix(line, "...") {
						t.Errorf("긴 제목 제한 미적용: %q", line)
					}
				}
			}

			if strings.Contains(title, "\r\n") && !strings.Contains(out, strings.ReplaceAll(title, "\r\n", " ")) {
				t.Errorf("제목 줄 정리 또는 Markdown 무력화 누락: %q", out)
			}
		})
	}
}

func TestSeedTemplates_OutboxVideoDistinguishesPremiereStates(t *testing.T) {
	pool := dbtest.NewPool(t)
	body := seedBody(t, pool, domain.TemplateKeyOutboxVideo)
	seen := make(map[string]bool)

	for _, tc := range []struct {
		kind               string
		premiere, upcoming bool
	}{
		{"LIVE_STREAM", false, false},
		{string(domain.OutboxKindNewVideo), false, false},
		{string(domain.OutboxKindNewVideo), true, false},
		{string(domain.OutboxKindNewVideo), true, true},
	} {
		out := renderSeedBody(t, domain.TemplateKeyOutboxVideo, body, map[string]any{
			"Kind": tc.kind, fieldIsPremiere: tc.premiere, "IsUpcomingPremiere": tc.upcoming,
			"MinutesUntilPremiere": 5, fieldMemberName: "미코", fieldTitle: "", fieldURL: "",
		})
		if seen[out] || !strings.Contains(out, "미코") {
			t.Errorf("알림 상태 구분 또는 대상 소실: %q", out)
		}

		seen[out] = true
	}
}
