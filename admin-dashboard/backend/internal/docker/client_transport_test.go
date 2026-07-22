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
				requireTransportError(t, tt.dockerHost, err)
				return
			}
			requireTransportSuccess(t, tt.dockerHost, baseURL, tt.wantBaseURL, transport, tt.wantUnix, err)
		})
	}
}

func TestDockerNetworkTransportsOwnConnectionPools(t *testing.T) {
	t.Parallel()

	_, first, err := dockerHTTPTransport("tcp://127.0.0.1:2375")
	if err != nil {
		t.Fatalf("first dockerHTTPTransport() error = %v", err)
	}
	_, second, err := dockerHTTPTransport("http://127.0.0.1:2375")
	if err != nil {
		t.Fatalf("second dockerHTTPTransport() error = %v", err)
	}
	if first == http.DefaultTransport || second == http.DefaultTransport || first == second {
		t.Fatal("Docker network transports must not share the process-global or each other's idle connection pool")
	}
}

func requireTransportError(t *testing.T, dockerHost string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("dockerHTTPTransport(%q) err = nil, want error", dockerHost)
	}
}

func requireTransportSuccess(t *testing.T, dockerHost, baseURL, wantBaseURL string, transport http.RoundTripper, wantUnix bool, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("dockerHTTPTransport(%q) err = %v", dockerHost, err)
	}
	if baseURL != wantBaseURL {
		t.Fatalf("baseURL = %q, want %q", baseURL, wantBaseURL)
	}
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", transport)
	}
	requireTransportKind(t, httpTransport, transport, wantUnix)
}

func requireTransportKind(t *testing.T, httpTransport *http.Transport, transport http.RoundTripper, wantUnix bool) {
	t.Helper()
	if transport == http.DefaultTransport {
		t.Fatal("Docker client must own its idle connection pool")
	}
	if wantUnix && httpTransport.DialContext == nil {
		t.Fatal("unix host must install a custom DialContext")
	}
}
