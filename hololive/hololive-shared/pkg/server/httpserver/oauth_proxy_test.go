package httpserver

import (
	"strings"
	"testing"
)

func TestBuildOAuthRedirectHTMLKeepsDeepLinkInHref(t *testing.T) {
	deepLink := BuildOAuthDeepLinkURL("abc", "xyz", "", "")
	html := BuildOAuthRedirectHTML(deepLink, false)

	if strings.Contains(html, "ZgotmplZ") {
		t.Fatalf("deep link sanitized to #ZgotmplZ, fallback button is dead:\n%s", html)
	}

	wantHref := `href="hololive-app://callback?code=abc&amp;state=xyz"`
	if !strings.Contains(html, wantHref) {
		t.Fatalf("rendered HTML missing %s:\n%s", wantHref, html)
	}

	if !strings.Contains(html, `window.location.href = "hololive-app:\/\/callback?code=abc\u0026state=xyz"`) &&
		!strings.Contains(html, `window.location.href = "hololive-app://callback?code=abc\u0026state=xyz"`) {
		t.Fatalf("rendered HTML missing JS redirect with deep link:\n%s", html)
	}
}

func TestBuildOAuthRedirectHTMLErrorParams(t *testing.T) {
	deepLink := BuildOAuthDeepLinkURL("", "", "access_denied", "user denied")
	html := BuildOAuthRedirectHTML(deepLink, true)

	if strings.Contains(html, "ZgotmplZ") {
		t.Fatalf("error deep link sanitized to #ZgotmplZ:\n%s", html)
	}

	if !strings.Contains(html, "error=access_denied") {
		t.Fatalf("rendered HTML missing error param:\n%s", html)
	}
}
