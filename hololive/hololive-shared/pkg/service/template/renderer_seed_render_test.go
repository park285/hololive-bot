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
	"slices"
	"strings"
	"testing"
	texttemplate "text/template"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

// 런타임 renderer는 missingkey 옵션 없이 파싱해 map 데이터의 키 누락이 `<no value>`로
// 조용히 노출된다(struct 필드 누락만 에러). 이 테스트가 명시적 missingkey=error로 전 키를
// 시드 본문 그대로 렌더해, 시드-샘플-변수 계약 위반을 배포 전에 유일하게 차단한다.

const (
	fieldMemberName = "MemberName"
	fieldTitle      = "Title"
	fieldURL        = "URL"
)

func TestSeedTemplates_RenderAllKeysWithSampleData(t *testing.T) {
	pool := dbtest.NewPool(t)

	rows, err := pool.Query(t.Context(),
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
		fieldMemberName: markerName,
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
			{"ChannelName": markerName, fieldTitle: markerName, fieldURL: markerURL, "ViewerCount": 0},
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
			{fieldMemberName: "사쿠라 미코", "TypesLabel": "라이브", "NextStream": liveNextStreamSample(markerTitle, streamURL)},
			{fieldMemberName: "호시마치 스이세이", "TypesLabel": "", "NextStream": liveNextStreamSample(markerTitle, "")},
			{fieldMemberName: "시라카미 후부키", "TypesLabel": "", "NextStream": liveNextStreamSample("", streamURL)},
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
			"Kind":          "NEW_VIDEO",
			fieldMemberName: markerMember,
			fieldTitle:      title,
			fieldURL:        url,
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
		fieldMemberName: "사쿠라 미코",
		"Kind":          "NEW_VIDEO",
		"Count":         3,
		"Items": []map[string]any{
			{fieldTitle: "", fieldURL: ""},
			{fieldTitle: "제목1", fieldURL: "https://youtu.be/v1"},
			{fieldTitle: "제목2", fieldURL: "https://youtu.be/v2"},
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
			{"Index": 1, "ChannelName": markerChannel, "ScheduledKST": "21:00", fieldTitle: markerTitle, fieldURL: streamURL},
			{"Index": 2, "ChannelName": markerChannel, "ScheduledKST": "", fieldTitle: markerTitle, fieldURL: ""},
			{"Index": 3, "ChannelName": "", "ScheduledKST": "", fieldTitle: "", fieldURL: streamURL},
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

func TestSeedTemplates_AlarmDispatchGroupPreservesShortLink(t *testing.T) {
	pool := dbtest.NewPool(t)

	const (
		markerTitle = "【挑戦】今日こそは絶対フルコン!!【 #UNIT_B / #玲銘ミラ】"
		shortURL    = "https://short.holoshi.com/l/CtQ_15HfY_M"
	)

	body := seedBody(t, pool, domain.TemplateKeyAlarmDispatchNotificationGroup)
	out := renderSeedBody(t, domain.TemplateKeyAlarmDispatchNotificationGroup, body, map[string]any{
		"MinutesUntil": 5,
		"IsStarting":   false,
		"AllPremiere":  false,
		"Entries": []map[string]any{
			{
				fieldMemberName:   "유닛 B",
				fieldTitle:        markerTitle,
				fieldURL:          shortURL,
				"CollabMembers":   "",
				"ScheduleMessage": "",
				"MinutesUntil":    5,
				"IsStarting":      false,
				"IsScheduled":     true,
				"IsPremiere":      false,
			},
		},
	})

	wantLink := "[" + util.MarkdownNeutralize(markerTitle) + "](" + shortURL + ")"
	if !hasSeedLine(out, wantLink) {
		t.Errorf("ALARM_DISPATCH_NOTIFICATION_GROUP: short-link label link %q 없음: %q", wantLink, out)
	}

	if !strings.Contains(out, shortURL) {
		t.Errorf("ALARM_DISPATCH_NOTIFICATION_GROUP: short URL이 변형됨: %q", out)
	}
}

func hasSeedLine(out, want string) bool {
	return slices.Contains(strings.Split(out, "\n"), want)
}

func liveNextStreamSample(title, url string) map[string]any {
	return map[string]any{
		"Status":       string(domain.NextStreamStatusLive),
		fieldTitle:     title,
		fieldURL:       url,
		"ScheduledKST": "",
		"TimeDetail":   "",
		"StartingSoon": false,
	}
}

func seedBody(t *testing.T, pool *pgxpool.Pool, key domain.TemplateKey) string {
	t.Helper()

	var body string

	if err := pool.QueryRow(t.Context(),
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

	if err := pool.QueryRow(t.Context(),
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
