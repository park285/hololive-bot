package sourcepolicy

import "testing"

func TestXAccountFromPath(t *testing.T) {
	t.Parallel()

	const account = "hololivetv"

	tests := []struct {
		path string
		want string
	}{
		{path: "/hololivetv", want: account},
		{path: "/hololivetv/", want: account},
		{path: "/HoloLiveTV/status/1", want: account},
		{path: "/@hololivetv", want: account},
		{path: "/%40hololivetv/status/1", want: account},
		{path: "/@", want: ""},
		{path: "/home", want: ""},
		{path: "/i/web/status/1", want: ""},
		{path: "/search", want: ""},
		{path: "/explore/", want: ""},
		{path: "/", want: ""},
		{path: "", want: ""},
		{path: "/hololivetv//status/1", want: ""},
		{path: "/hololivetv/../attacker", want: ""},
	}

	for _, tt := range tests {
		if got := XAccountFromPath(tt.path); got != tt.want {
			t.Errorf("XAccountFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestHostMatchesDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		host   string
		domain string
		want   bool
	}{
		{host: "hololivepro.com", domain: "hololivepro.com", want: true},
		{host: "shop.hololivepro.com", domain: "www.HololivePro.com", want: true},
		{host: "hololivepro.com.evil.example", domain: "hololivepro.com", want: false},
		{host: "evilhololivepro.com", domain: "hololivepro.com", want: false},
		{host: "example.com.", domain: " ", want: false},
	}

	for _, tt := range tests {
		if got := HostMatchesDomain(tt.host, tt.domain); got != tt.want {
			t.Errorf("HostMatchesDomain(%q, %q) = %v, want %v", tt.host, tt.domain, got, tt.want)
		}
	}
}
