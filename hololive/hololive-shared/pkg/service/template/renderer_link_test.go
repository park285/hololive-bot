package template

import (
	"strings"
	"testing"
	texttemplate "text/template"
)

// Migration 370 이전 알림 템플릿이 URL마다 인라인으로 계산하던 신뢰 링크 판정이다. 이 판정과 trustedlink의 결과가 같아야 표준 본문을
// 바꿔도 알림의 링크 표시가 달라지지 않는다.
const legacyAlarmURLBlock = `{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- if .URL}}
{{if $composite}}{{index $parts 0}}
{{index $parts 1}}{{else if $safeURL}}{{.URL}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}`

const trustedLinkURLBlock = `{{- if .URL}}
{{with trustedlink .URL}}{{.}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace $.URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}`

func TestTrustedLinkMatchesLegacyAlarmURLBlock(t *testing.T) {
	t.Parallel()

	urls := []string{
		"",
		"https://www.youtube.com/watch?v=abc123",
		"https://youtube.com/watch?v=abc",
		"https://m.youtube.com/watch?v=abc",
		"https://www.youtube.com/live/abc",
		"https://youtube.com/live/abc",
		"https://youtu.be/abc",
		"https://short.holoshi.com/l/abc",
		"https://www.twitch.tv/name",
		"https://twitch.tv/name",
		"https://chzzk.naver.com/live/abc",
		"https://youtu.be/abc | https://chzzk.naver.com/live/xyz",
		"https://short.holoshi.com/l/abc | https://chzzk.naver.com/live/xyz",
		"https://www.twitch.tv/name | https://chzzk.naver.com/live/xyz",
		"https://youtu.be/abc | https://youtu.be/def",
		"https://youtu.be/abc | https://chzzk.naver.com/live/x | https://chzzk.naver.com/live/y",
		"https://youtu.be/a b | https://chzzk.naver.com/live/xyz",
		"https://youtu.be/abc | https://chzzk.naver.com/live/x y",
		"https://youtu.be/abc|https://chzzk.naver.com/live/xyz",
		"https://youtu.be/a|b | https://chzzk.naver.com/live/xyz",
		"https://youtu.be/abc |  | https://chzzk.naver.com/live/xyz",
		"https://youtu.be/abc\nhttps://evil.example",
		"https://youtu.be/abc\r",
		"https://youtu.be/(abc)",
		"https://youtu.be/[abc]",
		"https://youtu.be/<abc>",
		"https://youtu.be/a\\b",
		"https://youtu.be/a\tb",
		"https://youtu.be/a b",
		"http://youtu.be/abc",
		"https://example.com/watch?v=abc",
		"https://youtube.com.evil.example/watch?v=abc",
		"**굵게** _기울임_ [링크](https://example.com)",
		"한글 URL 아님",
	}

	legacy := parseLinkBlock(t, "legacy", legacyAlarmURLBlock)
	helper := parseLinkBlock(t, "helper", trustedLinkURLBlock)

	for _, url := range urls {
		data := map[string]any{"URL": url}

		want := executeLinkBlock(t, legacy, data)
		if got := executeLinkBlock(t, helper, data); got != want {
			t.Errorf("URL %q: helper=%q legacy=%q", url, got, want)
		}
	}
}

func parseLinkBlock(t *testing.T, name, body string) *texttemplate.Template {
	t.Helper()

	tmpl, err := texttemplate.New(name).Funcs(templateFuncs).Parse(body)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	return tmpl
}

func executeLinkBlock(t *testing.T, tmpl *texttemplate.Template, data any) string {
	t.Helper()

	var out strings.Builder

	if err := tmpl.Execute(&out, data); err != nil {
		t.Fatalf("execute %s: %v", tmpl.Name(), err)
	}

	return out.String()
}
