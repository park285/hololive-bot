package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto/tls"
	"golang.org/x/net/http2"
)

func TestNewHTTPServer_SupportsH2C(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})

	server := newHTTPServer(":0", mux)
	if server == nil {
		t.Fatal("newHTTPServer() returned nil")
	}

	ts := httptest.NewUnstartedServer(server.Handler)
	ts.Start()
	defer ts.Close()

	client := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLS:   func(network, addr string, _ *tls.Config) (net.Conn, error) { return net.Dial(network, addr) },
		},
	}

	resp, err := client.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("h2c GET /health error = %v", err)
	}
	defer resp.Body.Close()

	if resp.ProtoMajor != 2 {
		t.Fatalf("response ProtoMajor = %d, want 2", resp.ProtoMajor)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
