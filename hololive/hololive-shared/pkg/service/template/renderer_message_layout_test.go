package template

import (
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

const testMixedMemberName = "미코 Miko"

func TestSeedTemplates_FinalTextPreservesLiteralTitles(t *testing.T) {
	pool := dbtest.NewPool(t)
	keys := []domain.TemplateKey{
		domain.TemplateKeyCmdLiveStreams, domain.TemplateKeyCmdUpcomingStreams, domain.TemplateKeyCmdChannelSchedule,
		domain.TemplateKeyCmdAlarmNotification, domain.TemplateKeyCmdAlarmLiveStarted, domain.TemplateKeyCmdAlarmNotificationGroup,
		domain.TemplateKeyOutboxVideo, domain.TemplateKeyOutboxShorts, domain.TemplateKeyOutboxVideoGroup,
		domain.TemplateKeyOutboxShortsGroup, domain.TemplateKeyCelebrationBirthdayStream, domain.TemplateKeyXSpaceStarted,
		domain.TemplateKeyAlarmDispatchNotification, domain.TemplateKeyAlarmDispatchNotificationGroup,
		domain.TemplateKeyOutboxCommunity, domain.TemplateKeyOutboxCommunityGroup,
		domain.TemplateKeyCmdMajorEventWeeklySummary, domain.TemplateKeyCmdMajorEventMonthlySummary,
		domain.TemplateKeyCmdMemberNewsDigest, domain.TemplateKeyCmdAlarmList, domain.TemplateKeyCmdAlarmAdded,
	}

	for _, key := range keys {
		body := seedBody(t, pool, key)

		for _, title := range []string{"**긴급** _今日_ ~~노래~~", "`문자 그대로`", "1. 오늘 방송", "- 오늘 방송", "> 오늘 방송", "첫 줄\r\n다음 줄\t제목", "#music [콜라보]", "Fish &amp; Chips <상영>", "연락처 <someone@example.invalid>", `C:\stream\\recording`} {
			t.Run(string(key)+"/"+title, func(t *testing.T) {
				const url = "https://youtu.be/a_b#c"

				out := kakaoformat.Render(renderSeedBody(t, key, body, literalTitleData(title, url)))
				visible := strings.ReplaceAll(out, util.KakaoZeroWidthSpace, "")
				want := strings.NewReplacer("\r\n", " ", "\t", " ").Replace(title)

				if !strings.Contains(visible, want) || !hasSeedLine(out, url) {
					t.Errorf("literal title or separate URL lost: %q", out)
				}

				if strings.ContainsRune(out, 0) || strings.Contains(out, "❪") || strings.Contains(out, "⦗") {
					t.Errorf("title interpreted as formatting: %q", out)
				}
			})
		}
	}
}

func literalTitleData(title, url string) map[string]any {
	entry := map[string]any{
		fieldTitle: title, fieldURL: url, fieldMemberName: testMixedMemberName, fieldChannelName: testMixedMemberName,
		"Index": 1, "IsLive": true, "TimeInfo": "시간 미정", fieldScheduledKST: "",
		"IsStarting": true, "IsScheduled": false, fieldIsPremiere: false, fieldMinutesUntil: 0,
		"CollabMembers": "", "ScheduleMessage": "",
		"ContentText": title, "Link": url, "DateStr": "미정", "Members": "",
		"Member": testMixedMemberName, "DateText": "미정", "Category": "노래", "Summary": "", "SourceURL": url,
		"TypesLabel": "라이브", fieldNextStream: liveNextStreamSample(title, url),
	}

	return map[string]any{
		fieldTitle: title, fieldURL: url, fieldMemberName: testMixedMemberName, fieldChannelName: testMixedMemberName,
		fieldCount: 1, "Hours": 24, "Days": 7,
		"Streams": []map[string]any{entry}, "Entries": []map[string]any{entry}, "Items": []map[string]any{entry},
		"ScheduledTimes": []string{}, "ScheduledTimeKST": "", "ScheduleMessage": "",
		"StreamTitle": title, "StreamURL": url, "ScheduledStartKST": "",
		"Kind": string(domain.OutboxKindNewVideo), "IsStarting": true, "AllPremiere": false, fieldIsPremiere: false,
		"IsUpcomingPremiere": false, "MinutesUntilPremiere": 0, fieldMinutesUntil: 0, "IsScheduled": false, "CollabMembers": "",
		"ContentText": title, "LLMSummary": "", "Events": []map[string]any{entry},
		"Headline": "뉴스", "TopItems": []map[string]any{entry}, "MoreSummary": "",
		"Alarms": []map[string]any{entry}, "Prefix": "!", "Added": true, fieldNextStream: liveNextStreamSample(title, url),
	}
}

func TestSeedTemplates_WorkerCompositeURLsAndScheduleLines(t *testing.T) {
	pool := dbtest.NewPool(t)

	const (
		primary   = "https://short.holoshi.com/l/CtQ_15HfY_M"
		secondary = "https://chzzk.naver.com/live/channel"
	)

	data := literalTitleData("방송 제목", primary+" | "+secondary)

	data["CollabMembers"] = "미코, 스이세이"
	data["ScheduleMessage"] = "변경 안내\r\n- 다음 시간에 시작"

	out := kakaoformat.Render(renderSeedBody(t, domain.TemplateKeyAlarmDispatchNotification,
		seedBody(t, pool, domain.TemplateKeyAlarmDispatchNotification), data))
	visible := strings.ReplaceAll(out, util.KakaoZeroWidthSpace, "")

	if !strings.HasSuffix(out, primary+"\n"+secondary) || !strings.Contains(visible, "콜라보: 미코, 스이세이\n변경 안내\n- 다음 시간에 시작") {
		t.Errorf("composite URL, collab, or schedule text changed: %q", out)
	}
}

func TestSeedTemplates_EmptyStatesDoNotInventEntries(t *testing.T) {
	pool := dbtest.NewPool(t)

	for _, tc := range []struct {
		key  domain.TemplateKey
		data map[string]any
		want string
	}{
		{domain.TemplateKeyCmdLiveStreams, map[string]any{fieldCount: 0}, "🔴 방송 중인 스트림이 없습니다."},
		{domain.TemplateKeyCmdUpcomingStreams, map[string]any{fieldCount: 0, "Hours": 24}, "📅 24시간 이내 예정된 방송이 없습니다."},
		{domain.TemplateKeyCmdChannelSchedule, map[string]any{fieldChannelName: ""}, "❌ 채널 정보를 찾을 수 없습니다."},
		{domain.TemplateKeyCmdChannelSchedule, map[string]any{fieldChannelName: "미코", fieldCount: 0, "Days": 7}, "📅 미코\n7일 이내 예정된 방송이 없습니다."},
		{domain.TemplateKeyCmdAlarmList, map[string]any{fieldCount: 0, "Prefix": "!"}, "🔔 설정된 알람이 없습니다.\n예) !알람 추가 페코라"},
		{domain.TemplateKeyCmdMemberNewsDigest, map[string]any{"Headline": " ", "TopItems": []any{}}, "📰 멤버 뉴스\n표시할 뉴스가 없습니다."},
		{domain.TemplateKeyCmdCalendar, map[string]any{fieldCount: 0, "Year": 2026, "Month": 9}, "📅 2026년 9월 등록된 기념일이 없습니다."},
		{domain.TemplateKeyCmdAlarmAdded, map[string]any{"Added": false, fieldMemberName: "미코"}, "ℹ️ 미코 알람이 이미 설정되어 있습니다."},
		{domain.TemplateKeyCmdAlarmAdded, map[string]any{"Added": true, fieldMemberName: "미코", fieldNextStream: map[string]any{"Status": "none"}}, "✅ 미코 알람을 설정했습니다.\n방송 시작 5분 전에 알립니다."},
		{domain.TemplateKeyCmdMemberDirectory, map[string]any{"Total": 1, "Groups": []map[string]any{
			{"GroupName": "그룹", "Members": []map[string]any{
				{"Primary": "", "Secondary": "", "ShowBoth": false},
				{"Primary": "미코", "Secondary": "", "ShowBoth": false},
			}},
		}}, "👤 멤버 목록 · 1명\n\n[그룹]\n· 미코"},
	} {
		t.Run(string(tc.key), func(t *testing.T) {
			out := kakaoformat.Render(renderSeedBody(t, tc.key, seedBody(t, pool, tc.key), tc.data))
			if out != tc.want {
				t.Errorf("empty-state meaning changed: got=%q want=%q", out, tc.want)
			}
		})
	}
}
