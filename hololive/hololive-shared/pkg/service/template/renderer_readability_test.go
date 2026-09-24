package template

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
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

			if strings.Contains(out, "❪") || strings.Contains(rendered, "**") || strings.Contains(rendered, "](") {
				t.Errorf("기본 강조나 제목 링크가 남음: %q", out)
			}

			// 한 줄짜리 동명이인 선택지는 간결한 목록을 유지합니다.
			if key != domain.TemplateKeyCmdAmbiguousMember && strings.Contains(out, "2 · ") && !strings.Contains(out, "\n\n──────────\n\n2 · ") {
				t.Errorf("항목 사이 빈 줄이 사라짐: %q", out)
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
				"Streams": []map[string]any{
					{fieldChannelName: "Kureiji Ollie Ch. hololive", fieldTitle: title, fieldURL: url, "ViewerCount": 987654},
					{fieldChannelName: "두 번째 채널", fieldTitle: "제목만 있음", fieldURL: "", "ViewerCount": 0},
					{fieldChannelName: "세 번째 채널", fieldTitle: "", fieldURL: "", "ViewerCount": 0},
				},
			})
			out := kakaoformat.Render(rendered)

			for _, required := range []string{"\n\n1 · Kureiji Ollie Ch. hololive", "\n" + url + "\n\n──────────\n\n2 · 두 번째 채널\n\u200b제목만 있음\n\n──────────\n\n3 · 세 번째 채널"} {
				if !strings.Contains(out, required) {
					t.Errorf("최종 출력에 필수 줄/간격 없음: %q", out)
				}
			}

			if strings.Contains(out, "987") || strings.Contains(out, "명)") || !utf8.ValidString(out) {
				t.Errorf("시청자 수 노출 또는 UTF-8 손상: %q", out)
			}

			for line := range strings.SplitSeq(out, "\n") {
				line = strings.TrimPrefix(line, util.KakaoZeroWidthSpace)

				if strings.HasPrefix(line, "긴 한글") || strings.HasPrefix(line, "長い日本語") || strings.HasPrefix(line, "A long") {
					if utf8.RuneCountInString(line) > 64 || !strings.HasSuffix(line, "...") {
						t.Errorf("긴 제목 제한 미적용: %q", line)
					}
				}
			}

			if strings.Contains(title, "\r\n") && !strings.Contains(rendered, util.MarkdownNeutralize(strings.ReplaceAll(title, "\r\n", " "))) {
				t.Errorf("제목 줄 정리 또는 Markdown 무력화 누락: %q", out)
			}
		})
	}
}

func TestSeedTemplates_AlarmKeepsBothProviderURLs(t *testing.T) {
	pool := dbtest.NewPool(t)

	const urls = "https://youtu.be/a_b#c\nhttps://chzzk.naver.com/live/example"

	for _, key := range []domain.TemplateKey{domain.TemplateKeyCmdAlarmNotification, domain.TemplateKeyCmdAlarmLiveStarted} {
		data := map[string]any{
			fieldChannelName: "테스트 채널", fieldTitle: "동시 송출", fieldURL: urls,
			"ScheduledTimeKST": "", "ScheduleMessage": "시간 미정",
		}
		out := kakaoformat.Render(renderSeedBody(t, key, seedBody(t, pool, key), data))

		if !strings.HasSuffix(out, "\n"+urls) {
			t.Errorf("%s: 동시 송출 URL 변형: %q", key, out)
		}

		if key == domain.TemplateKeyCmdAlarmNotification && (!hasSeedLine(out, "곧 시작") || !hasSeedLine(out, "시간 미정")) {
			t.Errorf("미정 일정 안내 소실: %q", out)
		}
	}
}

func TestSeedTemplates_OutboxVideoPremiereLabels(t *testing.T) {
	pool := dbtest.NewPool(t)
	body := seedBody(t, pool, domain.TemplateKeyOutboxVideo)

	for _, tc := range []struct {
		kind               string
		premiere, upcoming bool
		label              string
	}{
		{"LIVE_STREAM", false, false, "방송 시작"},
		{string(domain.OutboxKindNewVideo), true, false, "최초공개"},
		{string(domain.OutboxKindNewVideo), true, true, "5분 후 공개 예정"},
	} {
		out := renderSeedBody(t, domain.TemplateKeyOutboxVideo, body, map[string]any{
			"Kind": tc.kind, fieldIsPremiere: tc.premiere, "IsUpcomingPremiere": tc.upcoming,
			"MinutesUntilPremiere": 5, fieldMemberName: "미코", fieldTitle: "", fieldURL: "",
		})
		if !strings.HasSuffix(out, "미코 "+tc.label) {
			t.Errorf("알림 종류 구분 소실: %q", out)
		}
	}
}
