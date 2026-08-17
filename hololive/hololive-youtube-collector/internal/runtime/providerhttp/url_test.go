package providerhttp

import (
	"strings"
	"testing"
)

func TestHTTP001HolodexAPIPrefixJoinPathLive(t *testing.T) {
	t.Parallel()
	parsed, err := ParseHolodexBaseURL("https://holodex.net/api/v2")
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.JoinPath("live").String(); got != "https://holodex.net/api/v2/live" {
		t.Fatalf("JoinPath = %s", got)
	}
}

func TestHTTP002HolodexRejectsDirtyPaths(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"https://holodex.net/api/v2/../x",
		"https://holodex.net/api//v2",
		"https://holodex.net/api/v2/",
		"https://holodex.net/api/./v2",
	} {
		if _, err := ParseHolodexBaseURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestHTTP003OfficialRootAndEmptyPath(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"https://schedule.hololive.tv", "https://schedule.hololive.tv/"} {
		parsed, err := ParseOfficialScheduleBaseURL(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if got := parsed.JoinPath("api", "list", "2").String(); got != "https://schedule.hololive.tv/api/list/2" {
			t.Fatalf("JoinPath(%s) = %s", raw, got)
		}
	}
}

func TestHTTP004OfficialRejectsNonRootPath(t *testing.T) {
	t.Parallel()
	if _, err := ParseOfficialScheduleBaseURL("https://schedule.hololive.tv/api"); err == nil {
		t.Fatal("accepted official path prefix")
	}
}

func TestHTTP005UserinfoQueryFragmentRawPathRedacted(t *testing.T) {
	t.Parallel()
	const secret = "super-secret-userinfo"
	cases := []string{
		"https://user:" + secret + "@holodex.net/api/v2",
		"https://holodex.net/api/v2?token=" + secret,
		"https://holodex.net/api/v2#" + secret,
		"https://holodex.net/api%2Fv2",
	}
	for _, raw := range cases {
		_, err := ParseHolodexBaseURL(raw)
		if err == nil {
			t.Fatalf("accepted %s", raw)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("leaked secret in %q", err)
		}
		_, officialErr := ParseOfficialScheduleBaseURL(raw)
		if officialErr == nil {
			t.Fatalf("official accepted %s", raw)
		}
		if strings.Contains(officialErr.Error(), secret) {
			t.Fatalf("official leaked secret in %q", officialErr)
		}
	}
}
