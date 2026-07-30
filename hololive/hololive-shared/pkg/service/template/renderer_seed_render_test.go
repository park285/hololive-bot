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

package template

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	texttemplate "text/template"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

// 런타임 renderer는 missingkey 옵션 없이 파싱해 map 데이터의 키 누락이 `<no value>`로
// 조용히 노출된다(struct 필드 누락만 에러). 이 테스트가 명시적 missingkey=error로 전 키를
// 시드 본문 그대로 렌더해, 시드-샘플-변수 계약 위반을 배포 전에 유일하게 차단한다.
func TestSeedTemplates_RenderAllKeysWithSampleData(t *testing.T) {
	pool := dbtest.NewPool(t)

	rows, err := pool.Query(context.Background(),
		`SELECT template_key, body FROM notification_templates WHERE channel_id IS NULL`)
	if err != nil {
		t.Fatalf("query seeds: %v", err)
	}
	defer rows.Close()

	seeds := make(map[string]string)
	for rows.Next() {
		var key, body string
		if err := rows.Scan(&key, &body); err != nil {
			t.Fatalf("scan seed row: %v", err)
		}
		seeds[key] = body
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate seed rows: %v", err)
	}

	keys := sampledata.GetAllTemplateKeys()
	keyset := make(map[string]bool, len(keys))
	for _, key := range keys {
		keyset[string(key)] = true

		body, ok := seeds[string(key)]
		if !ok {
			t.Errorf("%s: 기본 시드 행이 없음", key)
			continue
		}

		data := sampledata.GetTemplateSampleData(key)
		if data == nil {
			t.Errorf("%s: sample data 없음", key)
			continue
		}

		tmpl, err := texttemplate.New(string(key)).Funcs(templateFuncs).Option("missingkey=error").Parse(body)
		if err != nil {
			t.Errorf("%s: parse 실패: %v", key, err)
			continue
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			t.Errorf("%s: sample data 렌더 실패: %v", key, err)
			continue
		}
		if strings.Contains(buf.String(), "<no value>") {
			t.Errorf("%s: 렌더 결과에 <no value> 노출", key)
		}
	}

	for key := range seeds {
		if !keyset[key] {
			t.Errorf("시드 키 %s가 GetAllTemplateKeys()에 없음 — 렌더 게이트 밖 키 금지", key)
		}
	}
}

func TestSeedTemplates_NeutralizeDynamicMarkdownFields(t *testing.T) {
	pool := dbtest.NewPool(t)

	const (
		markerName = "**미코**_[테스트]"
		markerURL  = "https://youtu.be/a_b#c"
	)

	notFoundBody := seedBody(t, pool, domain.TemplateKeyCmdMemberNotFound)
	notFound := renderSeedBody(t, domain.TemplateKeyCmdMemberNotFound, notFoundBody, map[string]any{
		"MemberName": markerName,
	})

	if !strings.Contains(notFound, util.MarkdownNeutralize(markerName)) {
		t.Errorf("CMD_MEMBER_NOT_FOUND: mdsafe 미적용: %q", notFound)
	}
	if strings.Contains(notFound, markerName) {
		t.Errorf("CMD_MEMBER_NOT_FOUND: 원본 마커가 그대로 노출: %q", notFound)
	}

	liveBody := seedBody(t, pool, domain.TemplateKeyCmdLiveStreams)
	live := renderSeedBody(t, domain.TemplateKeyCmdLiveStreams, liveBody, map[string]any{
		"Count": 1,
		"Streams": []map[string]any{
			{"ChannelName": markerName, "Title": markerName, "URL": markerURL, "ViewerCount": 0},
		},
	})

	wantLink := "[" + util.MarkdownNeutralize(markerName) + "](" + markerURL + ")"
	if !strings.Contains(live, wantLink) {
		t.Errorf("CMD_LIVE_STREAMS: 라벨 링크 %q 없음: %q", wantLink, live)
	}
	if !strings.Contains(live, markerURL) {
		t.Errorf("CMD_LIVE_STREAMS: URL이 변형됨(ZWSP 삽입 금지): %q", live)
	}
}

func TestSeedTemplates_AlarmListNextStreamLiveBranch(t *testing.T) {
	pool := dbtest.NewPool(t)

	const (
		markerTitle = "**콜라보**_[게릴라]"
		streamURL   = "https://youtu.be/a_b#c"
	)

	body := seedBody(t, pool, domain.TemplateKeyCmdAlarmList)
	out := renderSeedBody(t, domain.TemplateKeyCmdAlarmList, body, map[string]any{
		"Count":  3,
		"Prefix": "!",
		"Alarms": []map[string]any{
			{"MemberName": "사쿠라 미코", "TypesLabel": "라이브", "NextStream": liveNextStreamSample(markerTitle, streamURL)},
			{"MemberName": "호시마치 스이세이", "TypesLabel": "", "NextStream": liveNextStreamSample(markerTitle, "")},
			{"MemberName": "시라카미 후부키", "TypesLabel": "", "NextStream": liveNextStreamSample("", streamURL)},
		},
	})

	safeTitle := util.MarkdownNeutralize(markerTitle)
	lines := strings.Split(out, "\n")
	hasLine := func(want string) bool {
		return slices.Contains(lines, want)
	}

	if !hasLine("   🔴 방송 중") {
		t.Fatalf("CMD_ALARM_LIST: live 분기가 렌더되지 않음: %q", out)
	}
	if !hasLine("   [" + safeTitle + "](" + streamURL + ")") {
		t.Errorf("CMD_ALARM_LIST: 라벨 링크 분기 없음: %q", out)
	}
	if !hasLine("   " + safeTitle) {
		t.Errorf("CMD_ALARM_LIST: Title-only fallback 없음: %q", out)
	}
	if !hasLine("   " + streamURL) {
		t.Errorf("CMD_ALARM_LIST: URL-only fallback 없음: %q", out)
	}
	if strings.Contains(out, markerTitle) {
		t.Errorf("CMD_ALARM_LIST: 원본 마커가 그대로 노출: %q", out)
	}
}

func TestSeedTemplates_OutboxVideoLabelLinkBranches(t *testing.T) {
	pool := dbtest.NewPool(t)

	const (
		markerMember = "**미코**_[테스트]"
		markerTitle  = "~~신작~~ #미코라이브"
		videoURL     = "https://youtu.be/a_b#c"
	)

	body := seedBody(t, pool, domain.TemplateKeyOutboxVideo)
	render := func(title, url string) string {
		return renderSeedBody(t, domain.TemplateKeyOutboxVideo, body, map[string]any{
			"Kind":       "NEW_VIDEO",
			"MemberName": markerMember,
			"Title":      title,
			"URL":        url,
		})
	}

	safeMember := util.MarkdownNeutralize(markerMember)
	safeTitle := util.MarkdownNeutralize(markerTitle)
	wantHeader := "🔔 **" + safeMember + "** 새 영상"

	both := render(markerTitle, videoURL)
	if !hasSeedLine(both, wantHeader) {
		t.Errorf("OUTBOX_VIDEO: 헤더 라인 %q 없음: %q", wantHeader, both)
	}
	if !hasSeedLine(both, "["+safeTitle+"]("+videoURL+")") {
		t.Errorf("OUTBOX_VIDEO: 라벨 링크 분기 없음: %q", both)
	}
	if !strings.Contains(both, videoURL) {
		t.Errorf("OUTBOX_VIDEO: URL이 변형됨(ZWSP 삽입 금지): %q", both)
	}
	if strings.Contains(both, markerTitle) || strings.Contains(both, markerMember) {
		t.Errorf("OUTBOX_VIDEO: 원본 마커가 그대로 노출: %q", both)
	}

	titleOnly := render(markerTitle, "")
	if !hasSeedLine(titleOnly, safeTitle) {
		t.Errorf("OUTBOX_VIDEO: Title-only fallback 없음: %q", titleOnly)
	}
	if strings.Contains(titleOnly, "](") {
		t.Errorf("OUTBOX_VIDEO: URL 없이 라벨 링크가 생성됨: %q", titleOnly)
	}

	urlOnly := render("", videoURL)
	if !hasSeedLine(urlOnly, videoURL) {
		t.Errorf("OUTBOX_VIDEO: URL-only fallback 없음: %q", urlOnly)
	}
	if strings.Contains(urlOnly, "](") {
		t.Errorf("OUTBOX_VIDEO: Title 없이 라벨 링크가 생성됨: %q", urlOnly)
	}

	if got := render("", ""); got != wantHeader {
		t.Errorf("OUTBOX_VIDEO: Title/URL 모두 없을 때 = %q, want %q", got, wantHeader)
	}
}

func TestSeedTemplates_OutboxVideoGroupNumbersRenderedItems(t *testing.T) {
	pool := dbtest.NewPool(t)

	body := seedBody(t, pool, domain.TemplateKeyOutboxVideoGroup)
	out := renderSeedBody(t, domain.TemplateKeyOutboxVideoGroup, body, map[string]any{
		"MemberName": "사쿠라 미코",
		"Kind":       "NEW_VIDEO",
		"Count":      3,
		"Items": []map[string]any{
			{"Title": "", "URL": ""},
			{"Title": "제목1", "URL": "https://youtu.be/v1"},
			{"Title": "제목2", "URL": "https://youtu.be/v2"},
		},
	})

	want := "## 🔔 사쿠라 미코 새 영상 (3)\n1. [제목1](https://youtu.be/v1)\n2. [제목2](https://youtu.be/v2)"
	if out != want {
		t.Errorf("OUTBOX_VIDEO_GROUP: skip 항목 뒤 번호가 연속되지 않음\n got=%q\nwant=%q", out, want)
	}
}

func TestSeedTemplates_AlarmNotificationGroupEntryLabelLink(t *testing.T) {
	pool := dbtest.NewPool(t)

	const (
		markerChannel = "**스이세이**_[EN]"
		markerTitle   = "~~재방송~~ #노래방송"
		streamURL     = "https://youtu.be/a_b#c"
	)

	body := seedBody(t, pool, domain.TemplateKeyCmdAlarmNotificationGroup)
	out := renderSeedBody(t, domain.TemplateKeyCmdAlarmNotificationGroup, body, map[string]any{
		"Count":          3,
		"MinutesUntil":   5,
		"ScheduledTimes": []string{"21:00"},
		"Entries": []map[string]any{
			{"Index": 1, "ChannelName": markerChannel, "ScheduledKST": "21:00", "Title": markerTitle, "URL": streamURL},
			{"Index": 2, "ChannelName": markerChannel, "ScheduledKST": "", "Title": markerTitle, "URL": ""},
			{"Index": 3, "ChannelName": "", "ScheduledKST": "", "Title": "", "URL": streamURL},
		},
	})

	safeChannel := util.MarkdownNeutralize(markerChannel)
	safeTitle := util.MarkdownNeutralize(markerTitle)

	for _, want := range []string{
		"## 🔔 방송 알림 (3)",
		"⏰ 21:00",
		"1. **" + safeChannel + "** (21:00)",
		"   [" + safeTitle + "](" + streamURL + ")",
		"2. **" + safeChannel + "**",
		"   " + safeTitle,
		"3. **알 수 없는 채널**",
		"   " + streamURL,
	} {
		if !hasSeedLine(out, want) {
			t.Errorf("CMD_ALARM_NOTIFICATION_GROUP: 라인 %q 없음: %q", want, out)
		}
	}

	if strings.Contains(out, markerChannel) || strings.Contains(out, markerTitle) {
		t.Errorf("CMD_ALARM_NOTIFICATION_GROUP: 원본 마커가 그대로 노출: %q", out)
	}
	if !strings.Contains(out, streamURL) {
		t.Errorf("CMD_ALARM_NOTIFICATION_GROUP: URL이 변형됨(ZWSP 삽입 금지): %q", out)
	}
}

func TestSeedTemplates_RemainingMarkdownOutputs(t *testing.T) {
	pool := dbtest.NewPool(t)

	tests := []struct {
		name string
		key  domain.TemplateKey
		data any
		want string
	}{
		{
			name: "member news subscribed",
			key:  domain.TemplateKeyCmdMemberNewsSubscribed,
			data: map[string]any{},
			want: "✅ 뉴스 알림을 켰습니다.\n- 발송: **매주 월요일 09:00 KST**",
		},
		{
			name: "major event subscribed",
			key:  domain.TemplateKeyCmdMajorEventSubscribed,
			data: map[string]any{},
			want: "✅ 행사 알림을 켰습니다.\n- 발송: **매주 행사 요약**",
		},
		{
			name: "alarm cleared zero",
			key:  domain.TemplateKeyCmdAlarmCleared,
			data: map[string]any{"Count": 0},
			want: "🔔 설정된 알람이 없습니다.",
		},
		{
			name: "alarm cleared success",
			key:  domain.TemplateKeyCmdAlarmCleared,
			data: map[string]any{"Count": 3},
			want: "✅ 알람 **3개**를 모두 해제했습니다.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := seedBody(t, pool, tt.key)
			if got := renderSeedBody(t, tt.key, body, tt.data); got != tt.want {
				t.Errorf("%s 렌더 결과 = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestSeedTemplates_KnownSingleURLsUseLabelLinks(t *testing.T) {
	pool := dbtest.NewPool(t)

	tests := []struct {
		name      string
		key       domain.TemplateKey
		wantLines []string
		rawURLs   []string
	}{
		{
			name: "profile links",
			key:  domain.TemplateKeyCmdProfile,
			wantLines: []string{
				"- [음악 플레이리스트](https://www.youtube.com/playlist?list=example)",
				"- [Twitter](https://x.com/shirakamifubuki)",
				"[공식 프로필](https://hololive.hololivepro.com/talents/shirakami-fubuki)",
			},
			rawURLs: []string{
				"https://www.youtube.com/playlist?list=example",
				"https://x.com/shirakamifubuki",
				"https://hololive.hololivepro.com/talents/shirakami-fubuki",
			},
		},
		{
			name: "community post link",
			key:  domain.TemplateKeyOutboxCommunity,
			wantLines: []string{
				"[커뮤니티 글 보기](https://www.youtube.com/post/Ugkxyz123)",
			},
			rawURLs: []string{"https://www.youtube.com/post/Ugkxyz123"},
		},
		{
			name: "community group links",
			key:  domain.TemplateKeyOutboxCommunityGroup,
			wantLines: []string{
				"   [커뮤니티 글 보기](https://www.youtube.com/post/group-community-1)",
				"   [커뮤니티 글 보기](https://www.youtube.com/post/group-community-2)",
			},
			rawURLs: []string{
				"https://www.youtube.com/post/group-community-1",
				"https://www.youtube.com/post/group-community-2",
			},
		},
		{
			name: "birthday channel link",
			key:  domain.TemplateKeyCelebrationBirthday,
			wantLines: []string{
				"[YouTube 채널 보기](https://youtube.com/channel/UCdn5BQ06XqgXoAxIhbqw5Rg)",
			},
			rawURLs: []string{"https://youtube.com/channel/UCdn5BQ06XqgXoAxIhbqw5Rg"},
		},
		{
			name: "anniversary channel link",
			key:  domain.TemplateKeyCelebrationAnniversary,
			wantLines: []string{
				"[YouTube 채널 보기](https://youtube.com/channel/UCp6993wxpyDPHUpavwDFqgg)",
			},
			rawURLs: []string{"https://youtube.com/channel/UCp6993wxpyDPHUpavwDFqgg"},
		},
		{
			name: "alarm dispatch link",
			key:  domain.TemplateKeyAlarmDispatchNotification,
			wantLines: []string{
				"- [마인크래프트 건축](https://youtu.be/stream123)",
			},
			rawURLs: []string{"https://youtu.be/stream123"},
		},
		{
			name: "alarm dispatch group link",
			key:  domain.TemplateKeyAlarmDispatchNotificationGroup,
			wantLines: []string{
				"- [마인크래프트 건축](https://youtu.be/stream123)",
			},
			rawURLs: []string{"https://youtu.be/stream123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := seedBody(t, pool, tt.key)
			out := renderSeedBody(t, tt.key, body, sampledata.GetTemplateSampleData(tt.key))
			for _, want := range tt.wantLines {
				if !hasSeedLine(out, want) {
					t.Errorf("%s: label link line %q 없음: %q", tt.key, want, out)
				}
			}
			for _, rawURL := range tt.rawURLs {
				if hasSeedLine(out, rawURL) {
					t.Errorf("%s: raw URL line %q 노출: %q", tt.key, rawURL, out)
				}
			}
		})
	}
}

func TestSeedTemplates_AlarmDispatchPreservesRawURLFallbacks(t *testing.T) {
	pool := dbtest.NewPool(t)
	body := seedBody(t, pool, domain.TemplateKeyAlarmDispatchNotification)
	base := map[string]any{
		"IsStarting":      false,
		"IsScheduled":     false,
		"MemberName":      "비비",
		"MinutesUntil":    5,
		"ScheduleMessage": "",
	}

	for _, tt := range []struct {
		name  string
		title string
		url   string
		want  []string
		avoid []string
	}{
		{
			name:  "integrated stream",
			title: "동시송출 방송",
			url:   "https://youtube.com/watch?v=integrated | https://chzzk.naver.com/live/integrated",
			want: []string{
				"- 동시송출 방송",
				"- https://youtube.com/watch?v=integrated | https://chzzk.naver.com/live/integrated",
			},
		},
		{
			name: "url only",
			url:  "https://youtube.com/watch?v=url-only",
			want: []string{"- https://youtube.com/watch?v=url-only"},
		},
		{
			name:  "unexpected host",
			title: "의심 링크",
			url:   "https://evil.example/watch?v=malicious",
			want:  []string{"- 의심 링크", "- https://evil.example/watch?v=malicious"},
			avoid: []string{"[의심 링크]("},
		},
		{
			name:  "markdown injection",
			title: "위험 링크",
			url:   "https://www.youtube.com/watch?v=bad)\n[bad](https://evil.example)",
			want:  []string{"- 위험 링크"},
			avoid: []string{"[위험 링크](", "[bad]("},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := maps.Clone(base)
			data["Title"] = tt.title
			data["URL"] = tt.url
			out := renderSeedBody(t, domain.TemplateKeyAlarmDispatchNotification, body, data)
			for _, want := range tt.want {
				if !hasSeedLine(out, want) {
					t.Errorf("ALARM_DISPATCH_NOTIFICATION: fallback line %q 없음: %q", want, out)
				}
			}
			for _, avoid := range tt.avoid {
				if strings.Contains(out, avoid) {
					t.Errorf("ALARM_DISPATCH_NOTIFICATION: unsafe markdown %q 노출: %q", avoid, out)
				}
			}
		})
	}
}

func TestSeedTemplates_AlarmDispatchGroupPreservesCompositeURLs(t *testing.T) {
	pool := dbtest.NewPool(t)
	body := seedBody(t, pool, domain.TemplateKeyAlarmDispatchNotificationGroup)
	out := renderSeedBody(t, domain.TemplateKeyAlarmDispatchNotificationGroup, body, map[string]any{
		"IsStarting":   false,
		"MinutesUntil": 5,
		"Entries": []map[string]any{
			{
				"IsStarting":      false,
				"IsScheduled":     false,
				"MemberName":      "비비",
				"MinutesUntil":    5,
				"Title":           "동시송출 방송",
				"ScheduleMessage": "",
				"URL":             "https://youtube.com/watch?v=integrated | https://chzzk.naver.com/live/integrated",
			},
		},
	})

	for _, want := range []string{
		"- 동시송출 방송",
		"- https://youtube.com/watch?v=integrated | https://chzzk.naver.com/live/integrated",
	} {
		if !hasSeedLine(out, want) {
			t.Errorf("ALARM_DISPATCH_NOTIFICATION_GROUP: composite line %q 없음: %q", want, out)
		}
	}
}

func hasSeedLine(out, want string) bool {
	return slices.Contains(strings.Split(out, "\n"), want)
}

func liveNextStreamSample(title, url string) map[string]any {
	return map[string]any{
		"Status":       string(domain.NextStreamStatusLive),
		"Title":        title,
		"URL":          url,
		"ScheduledKST": "",
		"TimeDetail":   "",
		"StartingSoon": false,
	}
}

func seedBody(t *testing.T, pool *pgxpool.Pool, key domain.TemplateKey) string {
	t.Helper()

	var body string
	if err := pool.QueryRow(context.Background(),
		`SELECT body FROM notification_templates WHERE template_key = $1 AND channel_id IS NULL`,
		key,
	).Scan(&body); err != nil {
		t.Fatalf("query %s seed: %v", key, err)
	}
	return body
}

func renderSeedBody(t *testing.T, key domain.TemplateKey, body string, data any) string {
	t.Helper()

	tmpl, err := texttemplate.New(string(key)).Funcs(templateFuncs).Option("missingkey=error").Parse(body)
	if err != nil {
		t.Fatalf("%s: parse 실패: %v", key, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("%s: 렌더 실패: %v", key, err)
	}
	return buf.String()
}

func TestSeedTemplates_CommandHelpMentionsBroadcastHistory(t *testing.T) {
	pool := dbtest.NewPool(t)

	var body string
	if err := pool.QueryRow(context.Background(),
		`SELECT body FROM notification_templates WHERE template_key = $1 AND channel_id IS NULL`,
		domain.TemplateKeyCmdHelp,
	).Scan(&body); err != nil {
		t.Fatalf("query CMD_HELP seed: %v", err)
	}

	for _, token := range []string{"방송이력", "썸네일"} {
		if !strings.Contains(body, token) {
			t.Fatalf("CMD_HELP missing %q: %s", token, body)
		}
	}
}
