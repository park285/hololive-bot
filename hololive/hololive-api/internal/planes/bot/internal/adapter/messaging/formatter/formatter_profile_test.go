package formatter

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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
