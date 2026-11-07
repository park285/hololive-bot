package summarizer

import "testing"

func TestIsTrustedTextSocialToken(t *testing.T) {
	const token = "hololivetv"

	tests := []struct {
		source string
		want   bool
	}{
		{source: "@" + token, want: true},
		{source: "x.com/" + token, want: true},
		{source: "twitter.com/" + token, want: true},

		{source: token, want: false},
		{source: "@" + token + "extra", want: false},
		{source: "x.com/" + token + "/status/1", want: false},
		{source: "evil.com/" + token, want: false},
		{source: "https://evil.com/" + token, want: false},
		{source: "ftp://x.com/" + token, want: false},
		{source: "https://x.com/@" + token, want: false},
		{source: "http://https://x.com/" + token, want: false},
		{source: "https://http://x.com/" + token, want: false},
		{source: "https://https://x.com/" + token, want: false},
		{source: "", want: false},
	}

	for _, tt := range tests {
		if got := isTrustedTextSocialToken(tt.source, token); got != tt.want {
			t.Errorf("isTrustedTextSocialToken(%q, %q) = %v, want %v", tt.source, token, got, tt.want)
		}
	}
}

func TestTrustedDiscoveredSourceRejectsAmbiguousURLs(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"https://x.com/hololivetv/../attacker/status/1",
		"https://x.com/hololivetv/./status/1",
		"https://x.com/hololivetv/%2e%2e/attacker/status/1",
		"https://x.com/hololivetv/.%2E/attacker/status/1",
		"https://twitter.com/hololivetv/%2e./attacker/status/1",
		"https://x.com/hololivetv/%2e/status/1",
		`https://x.com/hololivetv/..\attacker/status/1`,
		"https://x.com/hololivetv/%5c../attacker/status/1",
		"https://x.com/hololivetv%2f../attacker/status/1",
		"https://x.com//hololivetv/status/1",
		"https://x.com/%20hololivetv/status/1",
		"https://x.com/hololivetv/%",
		"https:///x.com/hololivetv",
		"https://[x.com/hololivetv",
		"https:\\\\x.com\\hololivetv",
		"https://x.com:invalid/hololivetv",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			// URL은 외부 요청 없이 출처 메타데이터로만 검증한다.
			if isTrustedDiscoveredSource(source) {
				t.Errorf("ambiguous source accepted: %q", source)
			}

			if got := filterTrustedDiscoveredEvents([]discoveredEvent{{Name: "외부 행사", Source: source}}); len(got) != 0 {
				t.Errorf("ambiguous source survived event filtering: %#v", got)
			}
		})
	}
}

func TestTrustedDiscoveredSourcePreservesTrustedInputs(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"https://x.com/hololivetv/status/1",
		"http://twitter.com/hololive_en/status/1?lang=en#details",
		"https://www.x.com/hololivetv/",
		"https://hololive.hololivepro.com/news/1",
		"@hololivetv",
		"x.com/hololivetv",
		"twitter.com/hololivetv",
	} {
		if !isTrustedDiscoveredSource(source) {
			t.Errorf("trusted source rejected: %q", source)
		}
	}
}
