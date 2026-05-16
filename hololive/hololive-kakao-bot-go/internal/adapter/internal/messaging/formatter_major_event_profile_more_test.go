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

package messaging

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
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
	assert.Equal(t, util.KakaoSeeMorePadding, strings.Count(weekly, util.KakaoZeroWidthSpace))

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
	want := ErrorMessage(ErrDisplayMajorEventFailed)

	assert.Equal(t, want, formatter.FormatMajorEventSubscribed(t.Context()))
	assert.Equal(t, want, formatter.FormatMajorEventUnsubscribed(t.Context()))
	assert.Equal(t, want, formatter.FormatMajorEventAlreadySubscribed(t.Context()))
	assert.Equal(t, want, formatter.FormatMajorEventNotSubscribed(t.Context()))
	assert.Equal(t, want, formatter.FormatMajorEventStatus(t.Context(), true))
	assert.Equal(t, want, formatter.FormatMajorEventUsage(t.Context()))
}

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
	assert.Contains(t, formatProfileCatchphrase(raw, translated), DefaultEmoji.Speech)
	assert.Contains(t, formatProfileSummary(raw, translated), "홀로라이브")
	assert.Contains(t, formatProfileHighlights(translated), "하이라이트")
	assert.Len(t, getProfileDataEntries(raw, translated), 2)
	assert.Contains(t, formatProfileDataEntries(raw, translated), "특기")
	assert.Contains(t, formatProfileSocialLinks(raw), "음악 플레이리스트")
	assert.Contains(t, formatProfileOfficialURL(raw), "공식 프로필")
	assert.Equal(t, "공식 굿즈", socialLinkLabel("公式グッズ"))
	assert.Equal(t, "custom", socialLinkLabel("custom"))

	parts := parseDisplayNameComponents("시라카미 후부키 (Shirakami Fubuki) / FBK")
	require.NotEmpty(t, parts)
	assert.Contains(t, parts, "시라카미 후부키")
	assert.Contains(t, parts, "Shirakami Fubuki")
	assert.Contains(t, parts, "FBK")

	names := []string{"A"}
	addUniqueName(&names, "a")
	addUniqueName(&names, "B")
	assert.Equal(t, []string{"A", "B"}, names)

	header := buildTalentHeader(raw, translated)
	assert.Contains(t, header, "시라카미 후부키")

	formatter := NewResponseFormatter("!", nil)
	msg := formatter.FormatTalentProfile(raw, translated)
	assert.Contains(t, msg, "시라카미 후부키")
	assert.Contains(t, msg, "공식 프로필")
	assert.Equal(t, util.KakaoSeeMorePadding, strings.Count(msg, util.KakaoZeroWidthSpace))

	assert.Equal(t, ErrorMessage(ErrDisplayProfileDataFailed), formatter.FormatTalentProfile(nil, translated))
}
