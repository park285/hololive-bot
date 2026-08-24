package format

import (
	"bytes"
	"maps"
	"testing"
	"text/template"

	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
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
	"add":    func(a, b int) int { return a + b },
	"mdsafe": util.MarkdownNeutralize,
}

const zwsp = util.KakaoZeroWidthSpace

const (
	outboxBodyVideo = `{{if eq .Kind "LIVE_STREAM"}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else if .IsUpcomingPremiere}}🔔 **{{mdsafe .MemberName}}** {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 **{{mdsafe .MemberName}}** 최초공개{{else}}🔔 **{{mdsafe .MemberName}}** 새 영상{{end}}
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}`
	outboxBodyShorts = `🔔 **{{mdsafe .MemberName}}** 새 쇼츠
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}`
	outboxBodyCommunity = `🔔 **{{mdsafe .MemberName}}** 커뮤니티 글
{{- if .ContentText}}
{{mdsafe (truncate 100 .ContentText)}}
{{- end}}
{{- if .URL}}
[커뮤니티 글 보기]({{.URL}})
{{- end}}`
	outboxBodyMilestone = `🎉 **{{mdsafe .MemberName}}** {{mdsafe .Milestone}} 달성`

	outboxBodyVideoGroup = `## {{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe .MemberName}} 방송 시작 ({{.Count}}){{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe .MemberName}} 새 영상 ({{.Count}}){{else}}🔔 {{mdsafe .MemberName}} 알림 ({{.Count}}){{end}}
{{- $n := 0}}
{{- range $item := .Items}}
{{- if and $item.Title $item.URL}}
{{- $n = add $n 1}}
{{$n}}. [{{mdsafe (truncate 40 $item.Title)}}]({{$item.URL}})
{{- else if $item.Title}}
{{- $n = add $n 1}}
{{$n}}. {{mdsafe (truncate 40 $item.Title)}}
{{- else if $item.URL}}
{{- $n = add $n 1}}
{{$n}}. {{$item.URL}}
{{- end}}
{{- end}}`
	outboxBodyShortsGroup = `## 🔔 {{mdsafe .MemberName}} 새 쇼츠 ({{.Count}})
{{- $n := 0}}
{{- range $item := .Items}}
{{- if and $item.Title $item.URL}}
{{- $n = add $n 1}}
{{$n}}. [{{mdsafe (truncate 40 $item.Title)}}]({{$item.URL}})
{{- else if $item.Title}}
{{- $n = add $n 1}}
{{$n}}. {{mdsafe (truncate 40 $item.Title)}}
{{- else if $item.URL}}
{{- $n = add $n 1}}
{{$n}}. {{$item.URL}}
{{- end}}
{{- end}}`
	outboxBodyCommunityGroup = `## 🔔 {{mdsafe .MemberName}} 커뮤니티 글 ({{.Count}})
{{- $n := 0}}
{{- range $item := .Items}}
{{- if $item.ContentText}}
{{- $n = add $n 1}}
{{$n}}. {{mdsafe (truncate 40 $item.ContentText)}}
{{- if $item.URL}}
   [커뮤니티 글 보기]({{$item.URL}})
{{- end}}
{{- else if $item.URL}}
{{- $n = add $n 1}}
{{$n}}. [커뮤니티 글 보기]({{$item.URL}})
{{- end}}
{{- end}}`
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

func TestOutboxVideoGroupBodySkipsEmptyItems(t *testing.T) {
	t.Parallel()

	allEmpty := renderOutboxBody(t, outboxBodyVideoGroup, GroupedTemplateData{
		MemberName: "사쿠라 미코",
		Kind:       string(domain.OutboxKindMilestone),
		Count:      2,
		Items:      []GroupedItemData{{}, {}},
	})
	if want := "## 🔔 사쿠라 미코 알림 (2)"; allEmpty != want {
		t.Fatalf("all-empty render mismatch\n got=%q\nwant=%q", allEmpty, want)
	}

	mixed := renderOutboxBody(t, outboxBodyVideoGroup, GroupedTemplateData{
		MemberName: "사쿠라 미코",
		Kind:       string(domain.OutboxKindNewVideo),
		Count:      3,
		Items: []GroupedItemData{
			{},
			{Title: "제목1", URL: "https://youtu.be/v1"},
			{Title: "제목2", URL: "https://youtu.be/v2"},
		},
	})
	wantMixed := "## 🔔 사쿠라 미코 새 영상 (3)\n1. [제목1](https://youtu.be/v1)\n2. [제목2](https://youtu.be/v2)"

	if mixed != wantMixed {
		t.Fatalf("mixed render mismatch\n got=%q\nwant=%q", mixed, wantMixed)
	}
}

func TestOutboxGroupBodiesNumberRenderedItemsConsecutively(t *testing.T) {
	t.Parallel()

	shorts := renderOutboxBody(t, outboxBodyShortsGroup, GroupedTemplateData{
		MemberName: "사쿠라 미코",
		Kind:       string(domain.OutboxKindNewShort),
		Count:      3,
		Items: []GroupedItemData{
			{},
			{Title: "쇼츠1", URL: "https://www.youtube.com/shorts/s1"},
			{Title: "쇼츠2", URL: "https://www.youtube.com/shorts/s2"},
		},
	})
	wantShorts := "## 🔔 사쿠라 미코 새 쇼츠 (3)\n1. [쇼츠1](https://www.youtube.com/shorts/s1)\n2. [쇼츠2](https://www.youtube.com/shorts/s2)"

	if shorts != wantShorts {
		t.Fatalf("shorts group render mismatch\n got=%q\nwant=%q", shorts, wantShorts)
	}

	community := renderOutboxBody(t, outboxBodyCommunityGroup, GroupedTemplateData{
		MemberName: "사쿠라 미코",
		Kind:       string(domain.OutboxKindCommunityPost),
		Count:      3,
		Items: []GroupedItemData{
			{},
			{ContentText: "공지1", URL: "https://www.youtube.com/post/p1"},
			{ContentText: "공지2", URL: "https://www.youtube.com/post/p2"},
		},
	})
	wantCommunity := "## 🔔 사쿠라 미코 커뮤니티 글 (3)\n1. 공지1\n   [커뮤니티 글 보기](https://www.youtube.com/post/p1)\n2. 공지2\n   [커뮤니티 글 보기](https://www.youtube.com/post/p2)"

	if community != wantCommunity {
		t.Fatalf("community group render mismatch\n got=%q\nwant=%q", community, wantCommunity)
	}
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
			want: "🔔 **사쿠라 미코** 새 영상\n[마인크래프트 건축 배틀 #" + zwsp + "미코라이브](https://youtu.be/video123xyz)",
		},
		{
			name: "single/live_stream",
			body: outboxBodyVideo,
			data: sampleWithKind(t, domain.TemplateKeyOutboxVideo, "LIVE_STREAM"),
			want: "🔴 **사쿠라 미코** 방송 시작\n[마인크래프트 건축 배틀 #" + zwsp + "미코라이브](https://youtu.be/video123xyz)",
		},
		{
			name: "single/shorts",
			body: outboxBodyShorts,
			data: sampleWithKind(t, domain.TemplateKeyOutboxShorts, "NEW_SHORT"),
			want: "🔔 **사쿠라 미코** 새 쇼츠\n[새 쇼츠 제목 - 귀여운 미코치](https://www.youtube.com/shorts/abc123xyz)",
		},
		{
			name: "single/community",
			body: outboxBodyCommunity,
			data: sampleWithKind(t, domain.TemplateKeyOutboxCommunity, "COMMUNITY_POST"),
			want: "🔔 **사쿠라 미코** 커뮤니티 글\n오늘 밤 10시에 방송합니다! 많이 놀러오세요~" + zwsp + "\n[커뮤니티 글 보기](https://www.youtube.com/post/Ugkxyz123)",
		},
		{
			name: "single/milestone",
			body: outboxBodyMilestone,
			data: sampleWithKind(t, domain.TemplateKeyOutboxMilestone, "MILESTONE"),
			want: "🎉 **사쿠라 미코** 200만 달성",
		},
		{
			name: "group/new_video",
			body: outboxBodyVideoGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxVideoGroup, "NEW_VIDEO"),
			want: "## 🔔 사쿠라 미코 새 영상 (2)\n1. [마인크래프트 건축 배틀 #" + zwsp + "1](https://youtu.be/group-video-1)\n2. [마인크래프트 건축 배틀 #" + zwsp + "2](https://youtu.be/group-video-2)",
		},
		{
			name: "group/live_stream",
			body: outboxBodyVideoGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxVideoGroup, "LIVE_STREAM"),
			want: "## 🔴 사쿠라 미코 방송 시작 (2)\n1. [마인크래프트 건축 배틀 #" + zwsp + "1](https://youtu.be/group-video-1)\n2. [마인크래프트 건축 배틀 #" + zwsp + "2](https://youtu.be/group-video-2)",
		},
		{
			name: "group/default_milestone",
			body: outboxBodyVideoGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxVideoGroup, "MILESTONE"),
			want: "## 🔔 사쿠라 미코 알림 (2)\n1. [마인크래프트 건축 배틀 #" + zwsp + "1](https://youtu.be/group-video-1)\n2. [마인크래프트 건축 배틀 #" + zwsp + "2](https://youtu.be/group-video-2)",
		},
		{
			name: "group/shorts",
			body: outboxBodyShortsGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxShortsGroup, "NEW_SHORT"),
			want: "## 🔔 사쿠라 미코 새 쇼츠 (2)\n1. [오늘의 쇼츠 #" + zwsp + "1](https://www.youtube.com/shorts/group-1)\n2. [오늘의 쇼츠 #" + zwsp + "2](https://www.youtube.com/shorts/group-2)",
		},
		{
			name: "group/community",
			body: outboxBodyCommunityGroup,
			data: sampleWithKind(t, domain.TemplateKeyOutboxCommunityGroup, "COMMUNITY_POST"),
			want: "## 🔔 사쿠라 미코 커뮤니티 글 (2)\n1. 오늘 밤 10시 방송 공지\n   [커뮤니티 글 보기](https://www.youtube.com/post/group-community-1)\n2. 굿즈 판매 시작 안내\n   [커뮤니티 글 보기](https://www.youtube.com/post/group-community-2)",
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

func TestOutboxHeaderBodyRendersPremiereGoldens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data map[string]any
		want string
	}{
		{
			name: "upcoming",
			data: map[string]any{
				"Kind": "NEW_VIDEO", "MemberName": "아크로라", "Title": "최초공개 영상",
				"URL": "https://youtu.be/premiere", "IsPremiere": true,
				"IsUpcomingPremiere": true, "MinutesUntilPremiere": 30,
			},
			want: "🔔 **아크로라** 30분 후 공개 예정\n[최초공개 영상](https://youtu.be/premiere)",
		},
		{
			name: "after schedule",
			data: map[string]any{
				"Kind": "NEW_VIDEO", "MemberName": "아크로라", "Title": "최초공개 영상",
				"URL": "https://youtu.be/premiere", "IsPremiere": true,
				"IsUpcomingPremiere": false, "MinutesUntilPremiere": -1,
			},
			want: "🔔 **아크로라** 최초공개\n[최초공개 영상](https://youtu.be/premiere)",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := renderOutboxBody(t, outboxBodyVideo, test.data); got != test.want {
				t.Fatalf("render mismatch\n got=%q\nwant=%q", got, test.want)
			}
		})
	}
}
