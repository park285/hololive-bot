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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
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
👤 멤버 정보
{{- else -}}
👤 {{index .Names 0}}{{if gt (len .Names) 1}} ({{join (slice .Names 1) " / "}}){{end}}
{{- end}}
{{- if .Catchphrase}}
"{{.Catchphrase}}"
{{- end}}
{{- if .Summary}}
{{.Summary}}
{{- end}}
{{- if .Highlights}}

[하이라이트]
{{- range .Highlights}}
- {{.}}
{{- end}}
{{- end}}
{{- if .DataRows}}

[프로필]
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

[링크]
{{- range .SocialLinks}}
- {{.Label}}: {{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

공식 프로필: {{.OfficialURL}}
{{- end -}}`

func TestFormatMemberInfoWithoutEmbeddedProfile(t *testing.T) {
	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{domain.TemplateKeyCmdProfile: cmdProfileTestBody})
	formatter := NewResponseFormatter("!", renderer)
	member := &domain.Member{Name: "New Member", NameKo: "신규 멤버", Org: "New Org", Units: []string{"holoAN"}, ChannelID: "shared-channel", OfficialURL: "https://example.com/new"}
	got := formatter.FormatMemberInfo(t.Context(), member)

	for _, value := range []string{"신규 멤버", "New Member", "New Org", "holoAN", "활동 중", "https://example.com/new", "https://www.youtube.com/channel/shared-channel"} {
		if !strings.Contains(got, value) {
			t.Errorf("missing %q in %q", value, got)
		}
	}

	for _, value := range []string{"생일", "데뷔일", "하이라이트", "친구야!"} {
		if strings.Contains(got, value) {
			t.Errorf("unregistered detail %q in %q", value, got)
		}
	}

	member.IsGraduated = true
	if got = formatter.FormatMemberInfo(t.Context(), member); !strings.Contains(got, "졸업·활동 종료") {
		t.Fatalf("graduation missing: %q", got)
	}
}
