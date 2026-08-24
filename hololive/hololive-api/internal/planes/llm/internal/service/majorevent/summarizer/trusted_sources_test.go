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
		{source: "https://x.com/" + token, want: true},
		{source: "https://twitter.com/" + token, want: true},
		{source: "http://x.com/" + token, want: true},
		{source: "http://twitter.com/" + token, want: true},

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
