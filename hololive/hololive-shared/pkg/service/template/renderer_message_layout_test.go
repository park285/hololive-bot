package template

import (
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
)

const testMixedMemberName = "미코 Miko"

func TestSeedTemplates_FinalTextPreservesLiteralTitles(t *testing.T) {
	pool := dbtest.NewPool(t)
	keys := []domain.TemplateKey{
		domain.TemplateKeyCmdLiveStreams, domain.TemplateKeyCmdUpcomingStreams, domain.TemplateKeyCmdChannelSchedule,
		domain.TemplateKeyOutboxVideo, domain.TemplateKeyOutboxShorts, domain.TemplateKeyOutboxVideoGroup,
		domain.TemplateKeyOutboxShortsGroup, domain.TemplateKeyCelebrationBirthdayStream, domain.TemplateKeyXSpaceStarted,
		domain.TemplateKeyAlarmDispatchNotification, domain.TemplateKeyAlarmDispatchNotificationGroup,
		domain.TemplateKeyOutboxCommunity, domain.TemplateKeyOutboxCommunityGroup,
		domain.TemplateKeyCmdMajorEventWeeklySummary, domain.TemplateKeyCmdMajorEventMonthlySummary,
		domain.TemplateKeyCmdMemberNewsDigest,
	}

	for _, key := range keys {
		body := seedBody(t, pool, key)

		for _, title := range []string{"**긴급** _今日_ ~~노래~~", "`문자 그대로`", "1. 오늘 방송", "- 오늘 방송", "> 오늘 방송", "첫 줄\r\n다음 줄\t제목", "#music [콜라보]", "Fish &amp; Chips <상영>", "연락처 <someone@example.invalid>", `C:\stream\\recording`} {
			t.Run(string(key)+"/"+title, func(t *testing.T) {
				const url = "https://youtu.be/a_b#c"

				out := kakaoformat.Render(renderSeedBody(t, key, body, literalTitleData(title, url)))
				visible := strings.ReplaceAll(out, kakaotext.ZeroWidthSpace, "")
				want := strings.NewReplacer("\r\n", " ", "\t", " ").Replace(title)
				styled := strings.ReplaceAll(kakaoformat.Render("**"+kakaotext.MarkdownLiteral(want)+"**"), kakaotext.ZeroWidthSpace, "")

				if (!strings.Contains(visible, want) && !strings.Contains(visible, styled)) || !hasSeedLine(out, url) {
					t.Errorf("literal title or separate URL lost: %q", out)
				}

				if strings.ContainsRune(out, 0) || strings.Contains(out, "⦗") {
					t.Errorf("literal 제목이 코드로 해석됨: %q", out)
				}
			})
		}
	}
}

func literalTitleData(title, url string) map[string]any {
	entry := map[string]any{
		fieldTitle: title, fieldURL: url, fieldMemberName: testMixedMemberName, fieldChannelName: testMixedMemberName,
		"Index": 1, "IsLive": true, "TimeInfo": "시간 미정", fieldElapsed: "", fieldScheduledKST: "",
		"IsStarting": true, "IsScheduled": false, fieldIsPremiere: false, fieldMinutesUntil: 0,
		"CollabMembers": "", fieldScheduleMessage: "",
		"ContentText": title, "Link": url, "DateStr": "미정", "Members": "",
		"Member": testMixedMemberName, "DateText": "미정", "Category": "노래", "Summary": "", "SourceURL": url,
	}

	return map[string]any{
		fieldTitle: title, fieldURL: url, fieldMemberName: testMixedMemberName, fieldChannelName: testMixedMemberName,
		fieldCount: 1, "Hours": 24, "Days": 7,
		fieldStreams: []map[string]any{entry}, "Entries": []map[string]any{entry}, "Items": []map[string]any{entry},
		"ScheduledTimes": []string{}, "ScheduledTimeKST": "", fieldScheduleMessage: "",
		"StreamTitle": title, "StreamURL": url, "ScheduledStartKST": "",
		"Kind": string(domain.OutboxKindNewVideo), "IsStarting": true, "AllPremiere": false, fieldIsPremiere: false,
		"IsUpcomingPremiere": false, "MinutesUntilPremiere": 0, fieldMinutesUntil: 0, "IsScheduled": false, "CollabMembers": "",
		"ContentText": title, "LLMSummary": "", "Events": []map[string]any{entry},
		"Headline": "뉴스", "TopItems": []map[string]any{entry}, "MoreSummary": "",
	}
}

func TestSeedTemplates_WorkerCompositeURLsAndScheduleLines(t *testing.T) {
	pool := dbtest.NewPool(t)

	const (
		primary   = "https://short.holoshi.com/l/CtQ_15HfY_M"
		secondary = "https://chzzk.naver.com/live/channel"
		collab    = "미코, 스이세이"
		schedule  = "변경 안내\r\n- 다음 시간에 시작"
	)

	data := literalTitleData("방송 제목", primary+" | "+secondary)

	data["CollabMembers"] = collab
	data[fieldScheduleMessage] = schedule

	out := kakaoformat.Render(renderSeedBody(t, domain.TemplateKeyAlarmDispatchNotification,
		seedBody(t, pool, domain.TemplateKeyAlarmDispatchNotification), data))
	visible := strings.ReplaceAll(out, kakaotext.ZeroWidthSpace, "")

	if !hasSeedLine(out, primary) || !hasSeedLine(out, secondary) ||
		!strings.Contains(visible, collab) ||
		!strings.Contains(visible, strings.ReplaceAll(schedule, "\r\n", "\n")) {
		t.Errorf("composite URL, collab, or schedule text changed: %q", out)
	}
}

func TestSeedTemplates_EmptyStatesDoNotInventEntries(t *testing.T) {
	pool := dbtest.NewPool(t)

	const hidden = "표시되면 안 되는 항목"

	for _, tc := range []struct {
		key  domain.TemplateKey
		data map[string]any
	}{
		{domain.TemplateKeyCmdLiveStreams, map[string]any{fieldCount: 0, fieldStreams: []any{hidden}}},
		{domain.TemplateKeyCmdUpcomingStreams, map[string]any{fieldCount: 0, "Hours": 24, fieldStreams: []any{hidden}}},
		{domain.TemplateKeyCmdChannelSchedule, map[string]any{fieldChannelName: "", fieldStreams: []any{hidden}}},
		{domain.TemplateKeyCmdChannelSchedule, map[string]any{fieldChannelName: "미코", fieldCount: 0, "Days": 7, fieldStreams: []any{hidden}}},
		{domain.TemplateKeyCmdAlarmList, map[string]any{fieldCount: 0, "Prefix": "!", "Alarms": []any{hidden}}},
		{domain.TemplateKeyCmdMemberNewsDigest, map[string]any{"Headline": " ", "TopItems": []any{}, "MoreSummary": hidden}},
		{domain.TemplateKeyCmdCalendar, map[string]any{fieldCount: 0, "Year": 2026, "Month": 9, "Days": []any{hidden}}},
		{domain.TemplateKeyCmdMemberDirectory, map[string]any{"Total": 0, "Groups": []any{}}},
	} {
		t.Run(string(tc.key), func(t *testing.T) {
			out := kakaoformat.Render(renderSeedBody(t, tc.key, seedBody(t, pool, tc.key), tc.data))
			if strings.TrimSpace(out) == "" || strings.Contains(out, hidden) || strings.Contains(out, "<no value>") {
				t.Errorf("빈 상태에서 데이터가 만들어지거나 안내가 사라짐: %q", out)
			}
		})
	}
}
