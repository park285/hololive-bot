package holodexcollector

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/providerhttp"
)

func TestClientRespectsRetryAfter(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	client := holodexTestClient(t, server, "key")
	_, err := client.Fetch(context.Background())
	if collecterr.CodeOf(err) != collecterr.Cooldown {
		t.Fatalf("error = %v", err)
	}
	hint := collecterr.RetryOf(err)
	if hint.Kind() != collecterr.RetryAfter || hint.After() != 12*time.Second {
		t.Fatalf("retry hint = %#v", hint)
	}
}

func TestHTTP001HolodexAPIPrefixJoinPathLive(t *testing.T) {
	t.Parallel()
	var hit atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/live", func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
		if r.URL.Path != "/api/v2/live" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`[]`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	wrapped, err := providerhttp.WrapProviderHTTPDoer(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(wrapped, server.URL+"/api/v2", "key", 1024)
	if err != nil {
		t.Fatal(err)
	}
	body, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hit.Load() || string(body) != "[]" {
		t.Fatalf("hit=%t body=%q", hit.Load(), body)
	}
}

func TestHTTP002HolodexRejectsDirtyPaths(t *testing.T) {
	t.Parallel()
	wrapped, err := providerhttp.WrapProviderHTTPDoer(&http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"https://holodex.net/api/v2/../x",
		"https://holodex.net/api//v2",
		"https://holodex.net/api/v2/",
	} {
		if _, err := NewClient(wrapped, raw, "key", 1024); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestHTTP005HolodexUserinfoIsRedacted(t *testing.T) {
	t.Parallel()
	marker := "holodex-userinfo-redaction-marker"
	wrapped, err := providerhttp.WrapProviderHTTPDoer(&http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewClient(wrapped, "https://user:"+marker+"@holodex.net/api/v2", "key", 1024)
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTP006HolodexRedirectDoesNotForwardAPIKey(t *testing.T) {
	t.Parallel()
	var secondHits atomic.Int32
	var secondKey string
	second := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		secondKey = r.Header.Get("X-APIKEY")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(second.Close)
	var firstHits atomic.Int32
	first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		http.Redirect(w, r, second.URL+"/other", http.StatusFound)
	}))
	t.Cleanup(first.Close)
	client := holodexTestClient(t, first, "test-key")
	_, err := client.Fetch(context.Background())
	if collecterr.ClassOf(err) != collecterr.ClassProtocol {
		t.Fatalf("redirect = %v", err)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 0 || secondKey != "" {
		t.Fatalf("hits first=%d second=%d key=%q", firstHits.Load(), secondHits.Load(), secondKey)
	}
}

func TestHTTP007HolodexUnauthorizedIsConfiguration(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if _, err := w.Write([]byte(`{"error":"no"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	_, err := holodexTestClient(t, server, "key").Fetch(context.Background())
	if collecterr.CodeOf(err) != collecterr.Configuration || collecterr.ClassOf(err) != collecterr.ClassConfiguration {
		t.Fatalf("401 = %v", err)
	}
}

func TestHTTP009HolodexGzipBombHitsDecompressedCap(t *testing.T) {
	t.Parallel()
	payload := append(bytes.Repeat([]byte(" "), 8192), []byte("[]")...)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		if _, err := writer.Write(payload); err != nil {
			t.Errorf("write gzip response: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Errorf("close gzip response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	_, err := holodexTestClient(t, server, "key").Fetch(context.Background())
	if collecterr.CodeOf(err) != collecterr.ResponseTooLarge || collecterr.ClassOf(err) != collecterr.ClassResourceLimit {
		t.Fatalf("gzip bomb = %v", err)
	}
}

func TestHTTP012HolodexCloseDoesNotMutateInjectedClient(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	original := server.Client()
	original.Timeout = 3 * time.Second
	wrapped, err := providerhttp.WrapProviderHTTPDoer(original)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(wrapped, server.URL, "key", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if original.Timeout != 3*time.Second {
		t.Fatalf("timeout mutated to %s", original.Timeout)
	}
	if original.CheckRedirect != nil {
		t.Fatal("injected CheckRedirect was mutated")
	}
}

func TestHTTP016TLSFailureDoesNotLeakAPIKey(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`[]`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	key := "holodex-test-key"
	owned, err := providerhttp.NewProviderHTTPClient(providerhttp.ProviderTransportConfig{
		RequestTimeout:        time.Second,
		DialTimeout:           time.Second,
		TLSHandshakeTimeout:   time.Second,
		ResponseHeaderTimeout: time.Second,
		IdleConnTimeout:       time.Second,
		MaxConnsPerHost:       1,
		MaxIdleConnsPerHost:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := owned.Close(); closeErr != nil {
			t.Errorf("close client: %v", closeErr)
		}
	})
	client, err := NewClient(owned, server.URL, key, 1024)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected TLS failure")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("leaked api key: %v", err)
	}
}

func holodexTestClient(t *testing.T, server *httptest.Server, apiKey string) *Client {
	t.Helper()
	wrapped, err := providerhttp.WrapProviderHTTPDoer(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(wrapped, server.URL, apiKey, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
