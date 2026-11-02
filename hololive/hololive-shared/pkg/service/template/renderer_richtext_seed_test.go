package template

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
)

// loadTemplateFormatPairs는 현행 표준 본문을 oldBody=plain, newBody=richtext로 돌려준다.
// 358이 두 변형을 정하고, 359가 일부 키의 richtext 본문을 바꾼다.
func loadTemplateFormatPairs(tb testing.TB) map[domain.TemplateKey]templateSeedPair {
	tb.Helper()

	dir := os.DirFS(filepath.Join("..", "..", "..", "..", "hololive-api", "scripts", "migrations"))

	raw, err := fs.ReadFile(dir, "358_template_format_variants.sql")
	if err != nil {
		tb.Fatal(err)
	}

	rows := regexp.MustCompile(`(?s)\('([A-Z0-9_]+)', '[0-9a-f]{64}', \$plain\$(.*?)\$plain\$, \$rich\$(.*?)\$rich\$\)`).
		FindAllStringSubmatch(string(raw), -1)
	pairs := make(map[domain.TemplateKey]templateSeedPair, len(rows))

	for _, row := range rows {
		pairs[domain.TemplateKey(row[1])] = templateSeedPair{row[2], row[3]}
	}

	raw, err = fs.ReadFile(dir, "359_richtext_collapsed_summary.sql")
	if err != nil {
		tb.Fatal(err)
	}

	for _, row := range regexp.MustCompile(`(?s)\('([A-Z0-9_]+)', '[0-9a-f]{64}', \$rich\$(.*?)\$rich\$\)`).FindAllStringSubmatch(string(raw), -1) {
		key := domain.TemplateKey(row[1])

		pair, exists := pairs[key]
		if !exists {
			tb.Fatalf("359가 358에 없는 키를 바꿈: %s", key)
		}

		pair.newBody = row[2]
		pairs[key] = pair
	}

	return pairs
}

// DB 없이 strict parse와 제한 실행 경로에 현행 RichText 본문을 전달한다.
func TestRichTextSeedsPreviewAllKeys(t *testing.T) {
	pairs := loadTemplateFormatPairs(t)
	keys := sampledata.GetAllTemplateKeys()

	if len(pairs) != len(keys) {
		t.Fatalf("기본 템플릿과 지원 키 집합의 크기가 다름: %d / %d", len(pairs), len(keys))
	}

	for _, key := range keys {
		t.Run(string(key), func(t *testing.T) {
			pair, exists := pairs[key]
			if !exists {
				t.Fatal("지원 키의 기본 템플릿 누락")
			}

			data := sampledata.GetTemplateSampleData(key)
			if data == nil {
				t.Fatal("지원 키의 표본 데이터 누락")
			}

			tmpl, err := parseTemplateBody(string(key), pair.newBody, true)
			if err != nil {
				t.Fatal(err)
			}

			out, err := executeTemplate(t.Context(), tmpl, data)
			if err != nil {
				t.Fatal(err)
			}

			auditSeedRender(t, key, pair.newBody, data)

			if strings.Contains(out, "<no value>") {
				t.Fatalf("데이터가 누락됨: %q", out)
			}

			t.Logf("RICHTEXT_PREVIEW %s\n%s\nEND_PREVIEW", key, out)
		})
	}
}

func TestRichTextSeedsPreserveCopyableCommands(t *testing.T) {
	const prefix = "/!*"

	pairs := loadTemplateFormatPairs(t)

	for _, key := range []domain.TemplateKey{
		domain.TemplateKeyCmdHelp, domain.TemplateKeyCmdAmbiguousMember, domain.TemplateKeyCmdAlarmList,
		domain.TemplateKeyCmdMajorEventNotSub, domain.TemplateKeyCmdMajorEventStatus, domain.TemplateKeyCmdMajorEventUsage,
		domain.TemplateKeyCmdMemberNewsNoMembers, domain.TemplateKeyCmdMemberNewsStatus,
	} {
		pair := pairs[key]
		src, ok := sampledata.GetTemplateSampleData(key).(map[string]any)

		if !ok {
			t.Fatalf("명령 표본의 자료형 불일치: %s", key)
		}

		t.Run(string(key), func(t *testing.T) {
			data := maps.Clone(src)

			data["Prefix"] = prefix

			if key == domain.TemplateKeyCmdAlarmList {
				data[fieldCount] = 0
			}

			if _, exists := data["FirstName"]; exists {
				data["FirstName"] = "**복사할 이름**_[공식명]"
			}

			old := kakaoformat.Render(renderSeedBody(t, key, pair.oldBody, data))
			current := kakaoformat.Render(renderSeedBody(t, key, pair.newBody, data))
			commands := 0

			for line := range strings.SplitSeq(old, "\n") {
				if !strings.Contains(line, prefix) {
					continue
				}

				commands++

				if !hasSeedLine(current, line) {
					t.Errorf("복사 명령이 변형됨: %q\n%s", line, current)
				}
			}

			if commands == 0 {
				t.Fatal("명령 표본이 렌더되지 않음")
			}
		})
	}
}

func TestRichTextSeedsOutboxLiteralBoundaries(t *testing.T) {
	pairs := loadTemplateFormatPairs(t)

	const (
		title  = "**원제** [공식명] _표기_ 끝**"
		member = "**멤버**_[이름]"
		url    = "https://youtu.be/a_b#c"
	)

	for _, key := range []domain.TemplateKey{
		domain.TemplateKeyOutboxVideo, domain.TemplateKeyOutboxVideoGroup,
		domain.TemplateKeyOutboxShorts, domain.TemplateKeyOutboxShortsGroup,
		domain.TemplateKeyOutboxCommunity, domain.TemplateKeyOutboxCommunityGroup,
	} {
		t.Run(string(key), func(t *testing.T) {
			data := literalTitleData(title, url)

			data[fieldMemberName] = member

			out := renderSeedBody(t, key, pairs[key].newBody, data)
			plain := kakaoformat.Render(out)
			visible := strings.ReplaceAll(plain, kakaotext.ZeroWidthSpace, "")

			if !strings.Contains(visible, title) || !strings.Contains(visible, member) ||
				!hasSeedLine(out, url) || !hasSeedLine(plain, url) {
				t.Fatalf("동적 literal 경계 또는 복사 가능한 URL 소실: %q / %q", out, plain)
			}
		})
	}
}

func TestRichTextSeedsPreserveProtectedSummaryAndPadding(t *testing.T) {
	pairs := loadTemplateFormatPairs(t)
	// 호출자 경계에서 이미 보호된 요약과 접기 패딩을 템플릿이 다시 중화하지 않아야 한다.
	summary := `\*\*원제\*\*` + "\n" + strings.Repeat(kakaotext.ZeroWidthSpace, 500) + "\nhttps://example.invalid/a_b"

	for _, key := range []domain.TemplateKey{domain.TemplateKeyCmdMajorEventWeeklySummary, domain.TemplateKeyCmdMajorEventMonthlySummary} {
		t.Run(string(key), func(t *testing.T) {
			data := literalTitleData("행사 제목", "https://example.invalid/event")

			data["LLMSummary"] = summary

			out := renderSeedBody(t, key, pairs[key].newBody, data)

			if !strings.Contains(out, summary) {
				t.Fatalf("보호된 요약 또는 접기 패딩이 변형됨: %q", out)
			}

			if !hasSeedLine(kakaoformat.Render(out), "https://example.invalid/a_b") {
				t.Fatal("요약의 독립 URL 소실")
			}
		})
	}
}

func TestRichTextSeedsDistinguishSubscriptionAndAlarmStates(t *testing.T) {
	pairs := loadTemplateFormatPairs(t)

	for key, field := range map[domain.TemplateKey]string{
		domain.TemplateKeyCmdAlarmAdded:       "Added",
		domain.TemplateKeyCmdAlarmRemoved:     "Removed",
		domain.TemplateKeyCmdMajorEventStatus: "IsSubscribed",
		domain.TemplateKeyCmdMemberNewsStatus: "IsSubscribed",
	} {
		t.Run(string(key), func(t *testing.T) {
			sample, ok := sampledata.GetTemplateSampleData(key).(map[string]any)
			if !ok {
				t.Fatalf("상태 표본의 자료형 불일치: %s", key)
			}

			data := maps.Clone(sample)

			data[field] = false

			disabled := renderSeedBody(t, key, pairs[key].newBody, data)

			data[field] = true

			enabled := renderSeedBody(t, key, pairs[key].newBody, data)

			if disabled == enabled || strings.TrimSpace(disabled) == "" || strings.TrimSpace(enabled) == "" {
				t.Fatalf("상태 전환이 출력에 반영되지 않음: %q / %q", disabled, enabled)
			}
		})
	}
}

func TestRichTextSeedsNormalizedEmptyFields(t *testing.T) {
	pairs := loadTemplateFormatPairs(t)

	for _, key := range sampledata.GetAllTemplateKeys() {
		t.Run(string(key), func(t *testing.T) {
			body := pairs[key].newBody
			empty := renderSeedBody(t, key, body, richTextDisplayData(t, key, ""))

			for _, value := range []string{"", " \t\r\n ", "\u200b", " \u200b\t\u200b "} {
				out := renderSeedBody(t, key, body, richTextDisplayData(t, key, value))
				// 정규화 후 빈 표시값은 빈 문자열과 같은 분기를 타며 구분선·빈 강조를 만들지 않는다.
				if strings.Contains(out, "****") || kakaoformat.Render(out) != kakaoformat.Render(empty) {
					t.Errorf("정규화된 빈 입력이 가시 구문을 만듦: input=%q out=%q empty=%q", value, out, empty)
				}
			}
		})
	}
}

func richTextDisplayData(t *testing.T, key domain.TemplateKey, value string) map[string]any {
	t.Helper()

	data := literalTitleData(value, "https://example.invalid/item")

	data[fieldMemberName], data[fieldChannelName], data["Headline"] = value, value, value

	entries, ok := data[fieldStreams].([]map[string]any)
	if !ok {
		t.Fatal("방송 표본의 자료형 불일치")
	}

	entry := entries[0]

	entry[fieldMemberName], entry[fieldChannelName], entry["Member"], entry["Name"] = value, value, value, value
	data["Prefix"], data["CommandExample"], data["FirstName"] = "!", "알람 추가", value
	data["Added"], data["Removed"], data["IsSubscribed"] = false, false, false
	data["Ordinal"], data["Years"], data["ChannelID"] = 0, 1, ""
	data["Total"] = 1
	data["Alarms"] = []map[string]any{{fieldMemberName: value, "TypesLabel": ""}}
	data["Candidates"] = []map[string]any{{"Index": 1, "Name": value}}
	data["Groups"] = []map[string]any{{
		"GroupName": value,
		"Members":   []map[string]any{{"Primary": value, "Secondary": value, "ShowBoth": true}},
	}}
	data["Names"] = []string{value}
	data["Catchphrase"], data["Summary"], data["OfficialURL"] = "", "", ""
	data["Highlights"] = []string{}
	data["DataRows"], data["SocialLinks"] = []map[string]any{}, []map[string]any{}

	if key == domain.TemplateKeyCmdCalendar {
		data["Year"], data["Month"] = 2026, 10
		data["Days"] = []map[string]any{{
			"Month": 10, "Day": 28,
			"Entries": []map[string]any{{"IsBirthday": true, "Name": value, "Years": 1}},
		}}
	}

	return data
}

func TestRichTextSeedsCloseStaticEmphasis(t *testing.T) {
	pairs := loadTemplateFormatPairs(t)

	for _, key := range sampledata.GetAllTemplateKeys() {
		t.Run(string(key), func(t *testing.T) {
			// 입력에는 별표가 없으므로 CommonMark 변환 뒤 남는 **는 닫히지 않은 정적 강조다(구두점 뒤 조사 등).
			out := renderSeedBody(t, key, pairs[key].newBody, richTextDisplayData(t, key, "표시 대상"))
			plain := kakaoformat.Render(out)

			if strings.Contains(plain, "**") {
				t.Fatalf("정적 강조가 닫히지 않음: raw=%q plain=%q", out, plain)
			}

			const url = "https://example.invalid/item"

			if strings.Contains(out, url) && !hasSeedLine(plain, url) {
				t.Fatalf("일반 텍스트에서 독립 URL이 변형됨: %q", plain)
			}
		})
	}
}

func TestRichTextSeedsMultilineLiteralsRemainVisible(t *testing.T) {
	pairs := loadTemplateFormatPairs(t)

	const content = "- 안내\r\n    **원제**\r\n\t목록"

	for key, field := range map[domain.TemplateKey]string{
		domain.TemplateKeyCmdProfile:                     "Summary",
		domain.TemplateKeyCmdMemberNewsDigest:            "MoreSummary",
		domain.TemplateKeyAlarmDispatchNotification:      fieldScheduleMessage,
		domain.TemplateKeyAlarmDispatchNotificationGroup: fieldScheduleMessage,
	} {
		t.Run(string(key), func(t *testing.T) {
			data := richTextDisplayData(t, key, "표시 대상")

			data[field] = content

			entries, ok := data["Entries"].([]map[string]any)

			if !ok {
				t.Fatal("알림 표본의 자료형 불일치")
			}

			entries[0][field] = content

			out := kakaoformat.Render(renderSeedBody(t, key, pairs[key].newBody, data))

			if !strings.Contains(out, strings.ReplaceAll(content, "\r\n", "\n")) {
				t.Fatalf("여러 줄 literal의 기호·들여쓰기·줄바꿈이 변형됨: %q", out)
			}
		})
	}
}

// 오픈채팅은 긴 RichText를 머리말 묶음만 남기고 접으므로, 목록형 본문은 머리말 바로 다음 줄에 요약을 두고
// 그다음 줄을 비워 그 묶음을 끝낸다.
func TestRichTextSeedsSummarizeCollapsedPreview(t *testing.T) {
	pairs := loadTemplateFormatPairs(t)
	secondLine := func(t *testing.T, key domain.TemplateKey, data any) string {
		t.Helper()

		lines := strings.Split(renderSeedBody(t, key, pairs[key].newBody, data), "\n")
		if len(lines) < 3 || lines[2] != "" {
			t.Fatalf("요약 뒤에 빈 줄이 없음: %q", lines)
		}

		return lines[1]
	}

	for key, want := range map[domain.TemplateKey]string{
		domain.TemplateKeyCmdLiveStreams:                 "사쿠라 미코 · 오오조라 스바루",
		domain.TemplateKeyAlarmDispatchNotificationGroup: "사쿠라 미코 · 호시마치 스이세이",
		domain.TemplateKeyCmdUpcomingStreams:             "다음: 시라카미 후부키 · 30분 후",
		domain.TemplateKeyCmdChannelSchedule:             "지금 방송 중",
	} {
		t.Run(string(key), func(t *testing.T) {
			if got := secondLine(t, key, sampledata.GetTemplateSampleData(key)); got != want {
				t.Fatalf("요약 줄 = %q, want %q", got, want)
			}
		})
	}

	t.Run("빈 이름은 요약에서 뺀다", func(t *testing.T) {
		stream := func(name string) map[string]any {
			return map[string]any{"ChannelName": name, "Elapsed": "", "Title": "제목", "URL": ""}
		}
		data := map[string]any{"Count": 3, "Streams": []map[string]any{stream("페코라"), stream(" "), stream("미코")}}

		if got := secondLine(t, domain.TemplateKeyCmdLiveStreams, data); got != "페코라 · 미코" {
			t.Fatalf("요약 줄 = %q", got)
		}
	})

	t.Run("뉴스 제목의 이모지를 겹치지 않는다", func(t *testing.T) {
		key := domain.TemplateKeyCmdMemberNewsDigest
		out := renderSeedBody(t, key, pairs[key].newBody, sampledata.GetTemplateSampleData(key))

		if first, _, _ := strings.Cut(out, "\n"); first != "**🗞️ 이번주 구독 멤버 뉴스**" {
			t.Fatalf("머리말 = %q", first)
		}
	})
}
