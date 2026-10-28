package template

import (
	"maps"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"

	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
)

// DB 없이 실제 Preview와 제한 실행 경로에 migration의 새 본문을 전달한다.
func TestRichTextSeedsPreviewAllKeys(t *testing.T) {
	pairs := loadTemplateSeedPairs(t, "347_richtext_templates.sql")
	service := NewAdminService(nil, nil, nil)
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

			out, data, err := service.Preview(t.Context(), key, pair.newBody)
			if err != nil {
				t.Fatal(err)
			}

			auditSeedRender(t, key, pair.newBody, data)

			if !strings.Contains(out, "**") || strings.Contains(out, "<no value>") {
				t.Fatalf("의도한 강조가 없거나 데이터가 누락됨: %q", out)
			}

			t.Logf("RICHTEXT_PREVIEW %s\n%s\nEND_PREVIEW", key, out)
		})
	}
}

func TestRichTextSeedsPreserveCopyableCommands(t *testing.T) {
	const prefix = "/!*"

	pairs := loadTemplateSeedPairs(t, "347_richtext_templates.sql")

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
	pairs := loadTemplateSeedPairs(t, "347_richtext_templates.sql")

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
	pairs := loadTemplateSeedPairs(t, "347_richtext_templates.sql")
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
	pairs := loadTemplateSeedPairs(t, "347_richtext_templates.sql")

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
	pairs := loadTemplateSeedPairs(t, "347_richtext_templates.sql")

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

func TestRichTextSeedsPlainConsumerRendersStaticEmphasis(t *testing.T) {
	pairs := loadTemplateSeedPairs(t, "347_richtext_templates.sql")

	for _, key := range sampledata.GetAllTemplateKeys() {
		t.Run(string(key), func(t *testing.T) {
			// 입력에는 별표가 없으므로 일반 텍스트에 남는 **는 정적 강조의 미렌더링이다.
			out := renderSeedBody(t, key, pairs[key].newBody, richTextDisplayData(t, key, "표시 대상"))
			plain := kakaoformat.Render(out)

			if !strings.Contains(out, "**") || strings.Contains(plain, "**") {
				t.Fatalf("일반 텍스트 소비자가 정적 강조를 렌더하지 못함: raw=%q plain=%q", out, plain)
			}

			const url = "https://example.invalid/item"

			if strings.Contains(out, url) && !hasSeedLine(plain, url) {
				t.Fatalf("일반 텍스트에서 독립 URL이 변형됨: %q", plain)
			}
		})
	}
}

func TestRichTextSeedsMultilineLiteralsRemainVisible(t *testing.T) {
	pairs := loadTemplateSeedPairs(t, "347_richtext_templates.sql")

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
