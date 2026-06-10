package docker

import (
	"net/http"
	"testing"
)

func TestDockerHTTPTransport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dockerHost  string
		wantBaseURL string
		wantUnix    bool
		wantErr     bool
	}{
		{name: "unix socket", dockerHost: "unix:///var/run/docker.sock", wantBaseURL: "http://docker", wantUnix: true},
		{name: "tcp host", dockerHost: "tcp://docker-proxy:2375", wantBaseURL: "http://docker-proxy:2375"},
		{name: "http url trims trailing slash", dockerHost: "http://docker-proxy:2375/", wantBaseURL: "http://docker-proxy:2375"},
		{name: "https url", dockerHost: "https://docker-proxy:2376", wantBaseURL: "https://docker-proxy:2376"},
		{name: "unsupported scheme", dockerHost: "ssh://host", wantErr: true},
		{name: "empty", dockerHost: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			baseURL, transport, err := dockerHTTPTransport(tt.dockerHost)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("dockerHTTPTransport(%q) err = nil, want error", tt.dockerHost)
				}
				return
			}
			if err != nil {
				t.Fatalf("dockerHTTPTransport(%q) err = %v", tt.dockerHost, err)
			}
			if baseURL != tt.wantBaseURL {
				t.Fatalf("baseURL = %q, want %q", baseURL, tt.wantBaseURL)
			}
			httpTransport, ok := transport.(*http.Transport)
			if !ok {
				t.Fatalf("transport = %T, want *http.Transport", transport)
			}
			hasCustomDial := httpTransport.DialContext != nil && httpTransport != http.DefaultTransport
			if tt.wantUnix && !hasCustomDial {
				t.Fatal("unix host must install a custom DialContext")
			}
			if !tt.wantUnix && transport != http.DefaultTransport {
				t.Fatalf("non-unix host must reuse http.DefaultTransport, got %p", httpTransport)
			}
		})
	}
}
