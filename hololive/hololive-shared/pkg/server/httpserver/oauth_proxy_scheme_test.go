package httpserver

import (
	"strings"
	"testing"
)

func TestBuildOAuthRedirectHTMLRejectsForeignScheme(t *testing.T) {
	t.Parallel()

	for _, hostile := range []string{
		"javascript:alert(1)",
		"JAVASCRIPT:alert(1)",
		"data:text/html;base64,PHNjcmlwdD4=",
		"https://evil.example/callback",
		"://malformed",
	} {
		html := BuildOAuthRedirectHTML(hostile, false)
		if strings.Contains(html, hostile) {
			t.Fatalf("hostile deep link %q must not reach the rendered href", hostile)
		}
		if !strings.Contains(html, "hololive-app://callback") {
			t.Fatalf("rejected deep link %q must fall back to the canonical link, got:\n%s", hostile, html)
		}
	}
}

func TestBuildOAuthRedirectHTMLKeepsAppSchemeDeepLink(t *testing.T) {
	t.Parallel()

	deepLink := BuildOAuthDeepLinkURL("the-code", "the-state", "", "")
	html := BuildOAuthRedirectHTML(deepLink, false)

	if !strings.Contains(html, "code=the-code") || !strings.Contains(html, "state=the-state") {
		t.Fatalf("a hololive-app:// deep link must pass through unchanged, got:\n%s", html)
	}
}
