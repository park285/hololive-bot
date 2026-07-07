package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/park285/iris-client-go/iris"
)

func TestProvideIrisClient_UsesRuntimeBaseURLFile(t *testing.T) {
	ctx := context.Background()
	botToken := "bot-token"
	var primaryMu sync.Mutex
	primaryCalls := 0
	primary := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("primary server path = %q", r.URL.Path)
		}
		primaryMu.Lock()
		primaryCalls++
		primaryMu.Unlock()
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Fatalf("write primary response: %v", err)
		}
	}))
	defer primary.Close()

	var fallbackMu sync.Mutex
	fallbackCalls := 0
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("fallback server path = %q", r.URL.Path)
		}
		fallbackMu.Lock()
		fallbackCalls++
		fallbackMu.Unlock()
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Fatalf("write fallback response: %v", err)
		}
	}))
	defer fallback.Close()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte(primary.URL), 0o600); err != nil {
		t.Fatalf("write base url file: %v", err)
	}

	t.Setenv("IRIS_BASE_URL", fallback.URL)
	t.Setenv("IRIS_BOT_TOKEN", botToken)
	t.Setenv("IRIS_BASE_URL_FILE", baseURLFilePath)
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", testProviderBaseURLHost(t, primary.URL))

	client, err := ProvideIrisClient(nil, iris.WithHTTPClient(primary.Client()))
	if err != nil {
		t.Fatalf("provide iris client: %v", err)
	}

	if err := client.SendMessage(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("send message: %v", err)
	}

	primaryMu.Lock()
	if primaryCalls != 1 {
		t.Fatalf("primary calls = %d, want 1", primaryCalls)
	}
	primaryMu.Unlock()

	fallbackMu.Lock()
	if fallbackCalls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fallbackCalls)
	}
	fallbackMu.Unlock()
}

func TestProvideIrisClient_RejectsInvalidBaseURLFileAtConstruction(t *testing.T) {
	baseURLFilePath := filepath.Join(t.TempDir(), "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte("https://attacker.example/"), 0o600); err != nil {
		t.Fatalf("write base url file: %v", err)
	}

	t.Setenv("IRIS_BASE_URL", "https://iris.example")
	t.Setenv("IRIS_BOT_TOKEN", "bot-token")
	t.Setenv("IRIS_BASE_URL_FILE", baseURLFilePath)
	t.Setenv("IRIS_H3_SERVER_NAME", "iris.example")

	client, err := ProvideIrisClient(nil, iris.WithHTTPClient(&http.Client{}))
	if err == nil {
		t.Fatalf("ProvideIrisClient() error = nil, client = %T", client)
	}
	if !strings.Contains(err.Error(), "IRIS_BASE_URL_FILE") {
		t.Fatalf("ProvideIrisClient() error = %v, want IRIS_BASE_URL_FILE context", err)
	}
}

func TestProvideIrisClient_AllowsBaseURLFileWithoutFallbackURL(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("server path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Fatalf("write server response: %v", err)
		}
	}))
	defer server.Close()

	baseURLFilePath := filepath.Join(t.TempDir(), "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte(server.URL), 0o600); err != nil {
		t.Fatalf("write base url file: %v", err)
	}

	t.Setenv("IRIS_BASE_URL", "")
	t.Setenv("IRIS_BOT_TOKEN", "bot-token")
	t.Setenv("IRIS_BASE_URL_FILE", baseURLFilePath)
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", testProviderBaseURLHost(t, server.URL))

	client, err := ProvideIrisClient(nil, iris.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("provide iris client: %v", err)
	}

	if err := client.SendMessage(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("send message: %v", err)
	}
}

func testProviderBaseURLHost(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test base URL: %v", err)
	}
	return parsed.Hostname()
}

func TestProvideIrisClient_UsesExplicitOptionsOverEnvironment(t *testing.T) {
	ctx := context.Background()
	var explicitMu sync.Mutex
	explicitCalls := 0
	explicit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("explicit server path = %q", r.URL.Path)
		}
		explicitMu.Lock()
		explicitCalls++
		explicitMu.Unlock()
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Fatalf("write explicit response: %v", err)
		}
	}))
	defer explicit.Close()

	var envMu sync.Mutex
	envCalls := 0
	envServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("env server path = %q", r.URL.Path)
		}
		envMu.Lock()
		envCalls++
		envMu.Unlock()
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Fatalf("write env response: %v", err)
		}
	}))
	defer envServer.Close()

	t.Setenv("IRIS_BASE_URL", envServer.URL)
	t.Setenv("IRIS_BOT_TOKEN", "env-bot-token")
	t.Setenv("IRIS_BASE_URL_FILE", "")
	t.Setenv("IRIS_TRANSPORT", "h3")

	client, err := ProvideIrisClient(
		nil,
		iris.WithBaseURL(explicit.URL),
		iris.WithBotToken("explicit-bot-token"),
		iris.WithTransport("http1"),
		iris.WithHTTPClient(&http.Client{}),
	)
	if err != nil {
		t.Fatalf("provide iris client: %v", err)
	}

	if err := client.SendMessage(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("send message: %v", err)
	}

	explicitMu.Lock()
	if explicitCalls != 1 {
		t.Fatalf("explicit calls = %d, want 1", explicitCalls)
	}
	explicitMu.Unlock()

	envMu.Lock()
	if envCalls != 0 {
		t.Fatalf("env calls = %d, want 0", envCalls)
	}
	envMu.Unlock()
}
