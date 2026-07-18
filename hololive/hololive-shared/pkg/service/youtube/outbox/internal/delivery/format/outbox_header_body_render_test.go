package format

import (
	"bytes"
	"maps"
	"testing"
	"text/template"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template/sampledata"
)

var outboxRenderFuncs = template.FuncMap{
	"truncate": func(maxLen int, s string) string {
		runes := []rune(s)
		if len(runes) <= maxLen {
			return s
		}
		if maxLen <= 3 {
			return string(runes[:maxLen])
		}
		return string(runes[:maxLen-3]) + "..."
	},
	"add": func(a, b int) int { return a + b },
}

const (
	outboxBodyVideo     = "{{if eq .Kind \"LIVE_STREAM\"}}🔴 {{.MemberName}} 방송 시작{{else}}🔔 {{.MemberName}} 새 영상{{end}}\n{{.Title | truncate 50}}\n{{.URL}}"
	outboxBodyShorts    = "🔔 {{.MemberName}} 새 쇼츠\n{{.Title | truncate 50}}\n{{.URL}}"
	outboxBodyCommunity = "🔔 {{.MemberName}} 커뮤니티 글\n{{.ContentText | truncate 100}}\n{{.URL}}"
	outboxBodyMilestone = "🎉 {{.MemberName}} {{.Milestone}} 달성"

	outboxBodyVideoGroup = "{{if eq .Kind \"LIVE_STREAM\"}}🔴 {{.MemberName}} 방송 시작 ({{.Count}}){{else if eq .Kind \"NEW_VIDEO\"}}🔔 {{.MemberName}} 새 영상 ({{.Count}}){{else}}🔔 {{.MemberName}} 알림 ({{.Count}}){{end}}\n" +
		"{{range $idx, $item := .Items}}{{if gt $idx 0}}\n\n{{end}}{{add $idx 1}}. {{$item.Title | truncate 40}}\n   {{$item.URL}}{{end}}"
	outboxBodyShortsGroup = "🔔 {{.MemberName}} 새 쇼츠 ({{.Count}})\n" +
		"{{range $idx, $item := .Items}}{{if gt $idx 0}}\n\n{{end}}{{add $idx 1}}. {{$item.Title | truncate 40}}\n   {{$item.URL}}{{end}}"
	outboxBodyCommunityGroup = "🔔 {{.MemberName}} 커뮤니티 글 ({{.Count}})\n" +
		"{{range $idx, $item := .Items}}{{if gt $idx 0}}\n\n{{end}}{{add $idx 1}}. {{$item.ContentText | truncate 40}}\n   {{$item.URL}}{{end}}"
)

func renderOutboxBody(t *testing.T, body string, data any) string {
	t.Helper()
	tmpl, err := template.New("outbox").Funcs(outboxRenderFuncs).Option("missingkey=error").Parse(body)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return buf.String()
}

func sampleWithKind(t *testing.T, key domain.TemplateKey, kind string) map[string]any {
	t.Helper()
	src, ok := sampledata.GetTemplateSampleData(key).(map[string]any)
	if !ok {
		t.Fatalf("sample data for %s is not map[string]any", key)
	}
	out := make(map[string]any, len(src))
	maps.Copy(out, src)
	out["Kind"] = kind
	return out
}

func TestOutboxHeaderBodyRenderGoldens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		data map[string]any
		want string
	}{
		{
			name: "single/new_video",
			body: outboxBodyVideo,
			data: sampleWithKind(t, domain.TemplateKeyOutboxVideo, "NEW_VIDEO"),
			want: "🔔 사쿠라 미코 새 영상\n마인크래프트 건축 배틀 #미코라이브\nhttps://youtu.be/video123xyz",
		},
		{
			name: "single/live_stream",
			body: outboxBodyVideo,
			data: sampleWithKind(t, domain.TemplateKeyOutboxVideo, "LIVE_STREAM"),
			want: "🔴 사쿠라 미코 방송 시작\n마인크래프트 건축 배틀 #미코라이브\nhttps://youtu.be/video123xyz",
		},
		{
			name: "single/shorts",
			body: outboxBodyShorts,
			data: sampleWithKind(t, domain.TemplateKeyOutboxShorts, "NEW_SHORT"),
			want: "🔔 사쿠라 미코 새 쇼츠\n새 쇼츠 제목 - 귀여운 미코치\nhttps://www.youtube.com/shorts/abc123xyz",
		},
		{
			name: "single/community",
			body: outboxBodyCommunity,
			data: sampleWithKind(t, domain.TemplateKeyOutboxCommunity, "COMMUNITY_POST"),
			want: "🔔 사쿠라 미코 커뮤니티 글\n오늘 밤 10시에 방송합니다! 많이 놀러오세요~\nhttps://www.youtube.com/post/Ugkxyz123",
		},
		{
			name: "single/milestone",
			body: outboxBodyMilestone,
			data: sampleWithKind(t, domain.TemplateKeyOutboxMilestone, "MILESTONE"),
			want: "🎉 사쿠라 미코 200만 달성",
		},
		{
			name: "group/new_video",
			body: outboxBodyVideoGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxVideoGroup, "NEW_VIDEO"),
			want: "🔔 사쿠라 미코 새 영상 (2)\n1. 마인크래프트 건축 배틀 #1\n   https://youtu.be/group-video-1\n\n2. 마인크래프트 건축 배틀 #2\n   https://youtu.be/group-video-2",
		},
		{
			name: "group/live_stream",
			body: outboxBodyVideoGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxVideoGroup, "LIVE_STREAM"),
			want: "🔴 사쿠라 미코 방송 시작 (2)\n1. 마인크래프트 건축 배틀 #1\n   https://youtu.be/group-video-1\n\n2. 마인크래프트 건축 배틀 #2\n   https://youtu.be/group-video-2",
		},
		{
			name: "group/default_milestone",
			body: outboxBodyVideoGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxVideoGroup, "MILESTONE"),
			want: "🔔 사쿠라 미코 알림 (2)\n1. 마인크래프트 건축 배틀 #1\n   https://youtu.be/group-video-1\n\n2. 마인크래프트 건축 배틀 #2\n   https://youtu.be/group-video-2",
		},
		{
			name: "group/shorts",
			body: outboxBodyShortsGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxShortsGroup, "NEW_SHORT"),
			want: "🔔 사쿠라 미코 새 쇼츠 (2)\n1. 오늘의 쇼츠 #1\n   https://www.youtube.com/shorts/group-1\n\n2. 오늘의 쇼츠 #2\n   https://www.youtube.com/shorts/group-2",
		},
		{
			name: "group/community",
			body: outboxBodyCommunityGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxCommunityGroup, "COMMUNITY_POST"),
			want: "🔔 사쿠라 미코 커뮤니티 글 (2)\n1. 오늘 밤 10시 방송 공지\n   https://www.youtube.com/post/group-community-1\n\n2. 굿즈 판매 시작 안내\n   https://www.youtube.com/post/group-community-2",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := renderOutboxBody(t, c.body, c.data); got != c.want {
				t.Fatalf("render mismatch\n got=%q\nwant=%q", got, c.want)
			}
		})
	}
}
