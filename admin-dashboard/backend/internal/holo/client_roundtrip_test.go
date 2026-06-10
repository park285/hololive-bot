package holo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/park285/shared-go/pkg/json"
)

func TestProxyRoundTripOverHTTP(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath, gotQuery, gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAPIKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "secret-key")
	if err != nil {
		t.Fatalf("NewClient(%q) error = %v", server.URL, err)
	}

	resp, err := client.Proxy(context.Background(), http.MethodGet, "/channels", url.Values{"limit": {"5"}}, nil)
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Proxy() status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("body = %v, want ok=true", body)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("upstream method = %q, want GET", gotMethod)
	}
	if gotPath != "/channels" {
		t.Fatalf("upstream path = %q, want /channels", gotPath)
	}
	if gotQuery != "limit=5" {
		t.Fatalf("upstream query = %q, want limit=5", gotQuery)
	}
	if gotAPIKey != "secret-key" {
		t.Fatalf("upstream X-API-Key = %q, want secret-key", gotAPIKey)
	}
}

func TestProxyRoundTripMapsUpstreamErrorOverHTTP(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad input","code":"E_BAD"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatalf("NewClient(%q) error = %v", server.URL, err)
	}

	_, err = client.Proxy(context.Background(), http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("Proxy() error = nil, want upstream 400 mapped error")
	}
}

func TestProxyRoundTrip5xxBecomesBadGatewayOverHTTP(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatalf("NewClient(%q) error = %v", server.URL, err)
	}

	_, err = client.Proxy(context.Background(), http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("Proxy() error = nil, want bad gateway for upstream 5xx")
	}
}

func TestNewClientHTTPBranchUsesSharedInternalProfile(t *testing.T) {
	t.Parallel()

	client, err := NewClient("http://hololive-admin-api:30006", "k")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport for http:// base URL", client.http.Transport)
	}
	if client.http.Timeout != holoClientTimeout {
		t.Fatalf("client timeout = %v, want %v", client.http.Timeout, holoClientTimeout)
	}
	if transport.MaxIdleConnsPerHost != 32 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 32 (shared internal profile)", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != 64 {
		t.Fatalf("MaxConnsPerHost = %d, want 64 (shared internal profile)", transport.MaxConnsPerHost)
	}
	if transport.IdleConnTimeout != 90_000_000_000 {
		t.Fatalf("IdleConnTimeout = %v, want 90s", transport.IdleConnTimeout)
	}
}
