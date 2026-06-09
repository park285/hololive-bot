package delivery

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/iris"
)

func TestRuntimeIrisClientCloseClearsCachedClient(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewRuntimeIrisClient(server.URL, "bot-token", "", nil, iris.WithHTTPClient(server.Client()))
	if _, err := client.currentClient(); err != nil {
		t.Fatalf("currentClient() error = %v, want nil", err)
	}
	if client.cachedH2CClient == nil || client.cachedBaseURL == "" {
		t.Fatalf("cache not populated: client=%v baseURL=%q", client.cachedH2CClient, client.cachedBaseURL)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if client.cachedH2CClient != nil || client.cachedBaseURL != "" {
		t.Fatalf("cache not cleared: client=%v baseURL=%q", client.cachedH2CClient, client.cachedBaseURL)
	}
	if _, err := client.currentClient(); err == nil || !strings.Contains(err.Error(), "client is closed") {
		t.Fatalf("currentClient() after Close error = %v, want closed error", err)
	}
}

func TestRuntimeIrisClientDoesNotPoisonCacheWhenReloadedTransportCannotInitialize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewRuntimeIrisClient(server.URL, "bot-token", "", nil, iris.WithHTTPClient(server.Client()))
	if _, err := client.currentClient(); err != nil {
		t.Fatalf("initial currentClient() error = %v", err)
	}
	previous := client.cachedH2CClient

	bad := NewRuntimeIrisClient("https://iris.example", "bot-token", "", nil,
		iris.WithTransport("h3"),
		iris.WithH3CACertFile(filepath.Join(t.TempDir(), "missing-ca.pem")),
	)
	bad.cachedBaseURL = client.cachedBaseURL
	bad.cachedH2CClient = previous
	if _, err := bad.currentClient(); err == nil {
		t.Fatal("currentClient() error = nil, want H3 CA initialization error")
	}
	if bad.cachedH2CClient != previous || bad.cachedBaseURL != client.cachedBaseURL {
		t.Fatal("failed reload poisoned the previously cached client")
	}
}

func TestRuntimeIrisClientCloseFlushesPendingStaleClose(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewRuntimeIrisClient(server.URL, "bot-token", "", nil, iris.WithHTTPClient(server.Client()))
	// grace를 길게 잡아 stale close가 Close()의 신호 전까지 pending 상태로 남게 한다.
	client.staleCloseGrace = time.Hour

	stale := iris.NewH2CClient(server.URL, "bot-token", iris.WithHTTPClient(server.Client()))
	if err := stale.InitError(); err != nil {
		t.Fatalf("stale client init error = %v", err)
	}

	client.mu.Lock()
	client.scheduleStaleCloseLocked(stale)
	client.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- client.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close() did not flush pending stale close within 5s (closeSignal not honored)")
	}
}

func TestStaleCloseGraceExceedsReplyRetryBudget(t *testing.T) {
	t.Parallel()

	budget := runtimeIrisReplyAttemptTimeout * runtimeIrisReplyRetryMax
	if defaultStaleClientCloseGrace <= budget {
		t.Fatalf("defaultStaleClientCloseGrace=%s must exceed reply retry budget=%s (per-attempt %s × %d) so grace-close cannot sever an in-flight reply on base-URL rotation",
			defaultStaleClientCloseGrace, budget, runtimeIrisReplyAttemptTimeout, runtimeIrisReplyRetryMax)
	}
}
