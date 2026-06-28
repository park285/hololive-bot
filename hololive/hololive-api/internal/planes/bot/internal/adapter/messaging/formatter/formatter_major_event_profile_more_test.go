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

package formatter

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatMajorEventCommandMessages(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMajorEventWeeklySummary:  "주간 행사\n{{range .Events}}{{.Title}}|{{.DateStr}}|{{.Members}}|{{.Link}}\n{{end}}",
		domain.TemplateKeyCmdMajorEventSubscribed:     "구독완료 {{.Prefix}}",
		domain.TemplateKeyCmdMajorEventUnsubscribed:   "구독해제",
		domain.TemplateKeyCmdMajorEventAlreadySub:     "이미구독",
		domain.TemplateKeyCmdMajorEventNotSub:         "미구독 {{.Prefix}}",
		domain.TemplateKeyCmdMajorEventStatus:         "상태 {{if .IsSubscribed}}ON{{else}}OFF{{end}}",
		domain.TemplateKeyCmdMajorEventUsage:          "사용법 {{.Prefix}}행사알림",
		domain.TemplateKeyCmdMajorEventMonthlySummary: "월간 행사\n{{range .Events}}{{.Title}}\n{{end}}",
	})
	formatter := NewResponseFormatter("!", renderer)

	start := time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)
	events := []domain.MajorEvent{{
		Title:          "EXPO",
		EventStartDate: &start,
		EventEndDate:   &start,
		Members:        []string{"미코", "후부키"},
		Link:           "https://example.com/expo",
	}}

	weekly := formatter.FormatMajorEventWeeklySummary(t.Context(), events, "")
	assert.Contains(t, weekly, "주간 행사")
	assert.Contains(t, weekly, "EXPO")
	assert.Contains(t, weekly, "https://example.com/expo")
	assert.NotContains(t, weekly, "\u200b")

	assert.Equal(t, "구독완료 !", formatter.FormatMajorEventSubscribed(t.Context()))
	assert.Equal(t, "구독해제", formatter.FormatMajorEventUnsubscribed(t.Context()))
	assert.Equal(t, "이미구독", formatter.FormatMajorEventAlreadySubscribed(t.Context()))
	assert.Equal(t, "미구독 !", formatter.FormatMajorEventNotSubscribed(t.Context()))
	assert.Equal(t, "상태 ON", formatter.FormatMajorEventStatus(t.Context(), true))
	assert.Equal(t, "상태 OFF", formatter.FormatMajorEventStatus(t.Context(), false))
	assert.Equal(t, "사용법 !행사알림", formatter.FormatMajorEventUsage(t.Context()))
}

func TestFormatMajorEventCommandMessages_Fallback(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", setupFormatterTestRenderer(t, map[domain.TemplateKey]string{}))
	want := messagestrings.FallbackSentinel

	assert.Equal(t, want, formatter.FormatMajorEventSubscribed(t.Context()))
	assert.Equal(t, want, formatter.FormatMajorEventUnsubscribed(t.Context()))
	assert.Equal(t, want, formatter.FormatMajorEventAlreadySubscribed(t.Context()))
	assert.Equal(t, want, formatter.FormatMajorEventNotSubscribed(t.Context()))
	assert.Equal(t, want, formatter.FormatMajorEventStatus(t.Context(), true))
	assert.Equal(t, want, formatter.FormatMajorEventUsage(t.Context()))
}

const cmdProfileTestBody = `{{- if eq (len .Names) 0 -}}
📘 멤버 정보
{{- else -}}
📘 {{index .Names 0}}{{if gt (len .Names) 1}} ({{join (slice .Names 1) " / "}}){{end}}
{{- end}}
{{- if .Catchphrase}}
🗣️ {{.Catchphrase}}
{{- end}}
{{- if .Summary}}
{{.Summary}}
{{- end}}
{{- if .Highlights}}

✨ 하이라이트
{{- range .Highlights}}
- {{.}}
{{- end}}
{{- end}}
{{- if .DataRows}}

📋 프로필 데이터
{{- range .DataRows}}
{{- if .Multiline}}
- {{.Label}}:
{{.Value}}
{{- else}}
- {{.Label}}: {{.Value}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

🔗 링크
{{- range .SocialLinks}}
- {{.Label}}: {{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

🌐 공식 프로필: {{.OfficialURL}}
{{- end -}}`

const cmdProfileGolden = `📘 Shirakami Fubuki (시라카미 후부키 / 白上フブキ)
🗣️ 친구야!
홀로라이브 1기생

✨ 하이라이트
- 고양이 아님
- FOX

📋 프로필 데이터
- 생일: 10월 5일
- 특기:
  노래
  게임

🔗 링크
- 음악 플레이리스트: https://yt.example/playlist
- Twitter: https://x.example/fubuki

🌐 공식 프로필: https://hololive.example/fubuki`

func TestProfileHelpersAndFormatTalentProfile(t *testing.T) {
	t.Parallel()

	raw := &domain.TalentProfile{
		EnglishName:  "Shirakami Fubuki",
		JapaneseName: "白上フブキ",
		Catchphrase:  "Friend!",
		Description:  "Fox VTuber",
		DataEntries: []domain.TalentProfileEntry{
			{Label: "생일", Value: "10월 5일"},
			{Label: "", Value: "무시"},
		},
		SocialLinks: []domain.TalentSocialLink{
			{Label: "歌の再生リスト", URL: "https://yt.example/playlist"},
			{Label: "Twitter", URL: "https://x.example/fubuki"},
		},
		OfficialURL: "https://hololive.example/fubuki",
	}
	translated := &domain.Translated{
		DisplayName: "시라카미 후부키 (Shirakami Fubuki)",
		Catchphrase: "친구야!",
		Summary:     "홀로라이브 1기생",
		Highlights:  []string{"고양이 아님", "FOX"},
		Data: []domain.TranslatedProfileDataRow{
			{Label: "생일", Value: "10월 5일"},
			{Label: "특기", Value: "노래\n게임"},
		},
	}

	assert.Equal(t, "친구야!", getTranslatedText("친구야!", "Friend!"))
	assert.Equal(t, "Friend!", getTranslatedText(" ", "Friend!"))
	assert.Equal(t, "친구야!", profileCatchphrase(raw, translated))
	assert.Equal(t, "홀로라이브 1기생", profileSummary(raw, translated))
	assert.Equal(t, []string{"고양이 아님", "FOX"}, profileHighlights(translated))
	assert.Len(t, getProfileDataEntries(raw, translated), 2)

	rows := profileDataRows(raw, translated)
	require.Len(t, rows, 2)
	assert.Equal(t, "생일", rows[0].Label)
	assert.False(t, rows[0].Multiline)
	assert.Equal(t, "특기", rows[1].Label)
	assert.True(t, rows[1].Multiline)
	assert.Equal(t, "  노래\n  게임", rows[1].Value)
	assert.Equal(t, "https://hololive.example/fubuki", profileOfficialURL(raw))

	parts := parseDisplayNameComponents("시라카미 후부키 (Shirakami Fubuki) / FBK")
	require.NotEmpty(t, parts)
	assert.Contains(t, parts, "시라카미 후부키")
	assert.Contains(t, parts, "Shirakami Fubuki")
	assert.Contains(t, parts, "FBK")

	uniqueNames := []string{"A"}
	addUniqueName(&uniqueNames, "a")
	addUniqueName(&uniqueNames, "B")
	assert.Equal(t, []string{"A", "B"}, uniqueNames)

	assert.Contains(t, talentDisplayNames(raw, translated), "시라카미 후부키")

	store := setupFormatterTestStore(t)
	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdProfile: cmdProfileTestBody,
	})
	formatter := NewResponseFormatter("!", renderer, WithMessageStrings(store))

	assert.Equal(t, "공식 굿즈", formatter.socialLinkLabel(t.Context(), "公式グッズ"))
	assert.Equal(t, "custom", formatter.socialLinkLabel(t.Context(), "custom"))

	links := formatter.profileSocialLinks(t.Context(), raw)
	require.Len(t, links, 2)
	assert.Equal(t, "음악 플레이리스트", links[0].Label)
	assert.Equal(t, "https://yt.example/playlist", links[0].URL)
	assert.Equal(t, "Twitter", links[1].Label)

	msg := formatter.FormatTalentProfile(t.Context(), raw, translated)
	assert.Equal(t, cmdProfileGolden, msg)
	assert.NotContains(t, msg, "\u200b")

	assert.Equal(t, messagestrings.FallbackSentinel, formatter.FormatTalentProfile(t.Context(), nil, translated))
}
