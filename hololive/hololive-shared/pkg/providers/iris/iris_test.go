package iris

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	irisclient "github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type countingIrisServer struct {
	server *httptest.Server
	label  string
	mu     sync.Mutex
	calls  int
}

func newCountingIrisServer(t *testing.T, label string, useTLS bool) *countingIrisServer {
	t.Helper()

	counting := &countingIrisServer{label: label}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != irisclient.PathReply {
			t.Fatalf("%s server path = %q", label, r.URL.Path)
		}

		counting.mu.Lock()

		counting.calls++
		counting.mu.Unlock()
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(`{"ok":true}`)); err != nil {
			t.Fatalf("write %s response: %v", label, err)
		}
	})

	if useTLS {
		counting.server = httptest.NewTLSServer(handler)
	} else {
		counting.server = httptest.NewServer(handler)
	}

	t.Cleanup(counting.server.Close)

	return counting
}

func (s *countingIrisServer) assertCalls(t *testing.T, want int) {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.calls != want {
		t.Fatalf("%s calls = %d, want %d", s.label, s.calls, want)
	}
}

func TestProvideIrisClient_UsesRuntimeBaseURLFile(t *testing.T) {
	ctx := t.Context()
	primary := newCountingIrisServer(t, "primary", true)
	fallback := newCountingIrisServer(t, "fallback", false)

	baseURLFilePath := filepath.Join(t.TempDir(), "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte(primary.server.URL), 0o600); err != nil {
		t.Fatalf("write base url file: %v", err)
	}

	irisConfig := &settings.IrisConfig{
		BaseURL: fallback.server.URL, BotToken: "bot-token", BaseURLFile: baseURLFilePath,
		BaseURLAllowedHosts: []string{testProviderBaseURLHost(t, primary.server.URL)},
	}

	client, err := ProvideIrisClient(t.Context(), irisConfig, nil, irisclient.WithHTTPClient(primary.server.Client()))
	if err != nil {
		t.Fatalf("provide iris client: %v", err)
	}

	if _, err := client.SendMessageAccepted(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("send message: %v", err)
	}

	primary.assertCalls(t, 1)
	fallback.assertCalls(t, 0)
}

func TestProvideIrisClient_RejectsInvalidBaseURLFileAtConstruction(t *testing.T) {
	baseURLFilePath := filepath.Join(t.TempDir(), "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte("https://attacker.example/"), 0o600); err != nil {
		t.Fatalf("write base url file: %v", err)
	}

	irisConfig := &settings.IrisConfig{BaseURL: "https://iris.example", BotToken: "bot-token", BaseURLFile: baseURLFilePath, H3ServerName: "iris.example"}

	client, err := ProvideIrisClient(t.Context(), irisConfig, nil, irisclient.WithHTTPClient(&http.Client{}))
	if err == nil {
		t.Fatalf("ProvideIrisClient() error = nil, client = %T", client)
	}

	if client != nil {
		t.Fatalf("ProvideIrisClient() client = %T, want untyped nil interface on error", client)
	}

	if !strings.Contains(err.Error(), "IRIS_BASE_URL_FILE") {
		t.Fatalf("ProvideIrisClient() error = %v, want IRIS_BASE_URL_FILE context", err)
	}
}

func TestProvideIrisClient_AllowsBaseURLFileWithoutFallbackURL(t *testing.T) {
	ctx := t.Context()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != irisclient.PathReply {
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

	irisConfig := &settings.IrisConfig{
		BaseURL: "", BotToken: "bot-token", BaseURLFile: baseURLFilePath,
		BaseURLAllowedHosts: []string{testProviderBaseURLHost(t, server.URL)},
	}

	client, err := ProvideIrisClient(t.Context(), irisConfig, nil, irisclient.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("provide iris client: %v", err)
	}

	if _, err := client.SendMessageAccepted(ctx, "room-1", "hello"); err != nil {
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
