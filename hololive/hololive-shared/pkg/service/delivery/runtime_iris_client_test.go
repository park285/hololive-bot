package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/park285/iris-client-go/iris"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func TestRuntimeIrisClient_SendMessage_UsesBaseURLFileOverrideAndReloads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	botToken := "bot-token"
	var firstMu sync.Mutex
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("first server path = %q", r.URL.Path)
		}
		firstMu.Lock()
		firstCalls++
		firstMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer first.Close()

	var secondMu sync.Mutex
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("second server path = %q", r.URL.Path)
		}
		secondMu.Lock()
		secondCalls++
		secondMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer second.Close()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte(first.URL), 0o600); err != nil {
		t.Fatalf("write first base url file: %v", err)
	}

	client := NewRuntimeIrisClient(second.URL, botToken, baseURLFilePath, nil, iris.WithHTTPClient(&http.Client{}))

	if err := client.SendMessage(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("send via first override: %v", err)
	}

	firstMu.Lock()
	if firstCalls != 1 {
		t.Fatalf("first calls after first send = %d, want 1", firstCalls)
	}
	firstMu.Unlock()

	if err := os.WriteFile(baseURLFilePath, []byte(second.URL), 0o600); err != nil {
		t.Fatalf("write second base url file: %v", err)
	}

	if err := client.SendMessage(ctx, "room-1", "world"); err != nil {
		t.Fatalf("send via reloaded override: %v", err)
	}

	secondMu.Lock()
	if secondCalls != 1 {
		t.Fatalf("second calls after reload = %d, want 1", secondCalls)
	}
	secondMu.Unlock()
}

func TestRuntimeIrisClient_SendMessage_FallsBackWhenBaseURLFileMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	botToken := "bot-token"
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
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer fallback.Close()

	client := NewRuntimeIrisClient(fallback.URL, botToken, filepath.Join(t.TempDir(), "missing"), nil, iris.WithHTTPClient(&http.Client{}))
	if err := client.SendMessage(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("send via fallback: %v", err)
	}

	fallbackMu.Lock()
	if fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallbackCalls)
	}
	fallbackMu.Unlock()
}

func TestRuntimeIrisClient_SendMessage_FallsBackWhenBaseURLFileIsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	botToken := "bot-token"
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
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer fallback.Close()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte(" \n"), 0o600); err != nil {
		t.Fatalf("write empty base url file: %v", err)
	}

	client := NewRuntimeIrisClient(fallback.URL, botToken, baseURLFilePath, nil, iris.WithHTTPClient(&http.Client{}))
	if err := client.SendMessage(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("send via fallback after empty file: %v", err)
	}

	fallbackMu.Lock()
	if fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallbackCalls)
	}
	fallbackMu.Unlock()
}

func TestRuntimeIrisClient_SendMessage_FallsBackWhenBaseURLFileIsInvalid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	botToken := "bot-token"
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
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer fallback.Close()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte("http:// bad"), 0o600); err != nil {
		t.Fatalf("write invalid base url file: %v", err)
	}

	client := NewRuntimeIrisClient(fallback.URL, botToken, baseURLFilePath, nil, iris.WithHTTPClient(&http.Client{}))
	if err := client.SendMessage(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("send via fallback after invalid file: %v", err)
	}

	fallbackMu.Lock()
	if fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallbackCalls)
	}
	fallbackMu.Unlock()
}

func TestRuntimeIrisClient_SendMessage_FallsBackWhenH3BaseURLFileUsesHTTP(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "h3")

	ctx := context.Background()
	botToken := "bot-token"
	var fallbackMu sync.Mutex
	fallbackCalls := 0
	fallback := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("fallback server path = %q", r.URL.Path)
		}
		fallbackMu.Lock()
		fallbackCalls++
		fallbackMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer fallback.Close()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte("http://stale-iris.example"), 0o600); err != nil {
		t.Fatalf("write stale base url file: %v", err)
	}

	client := NewRuntimeIrisClient(fallback.URL, botToken, baseURLFilePath, nil, iris.WithHTTPClient(fallback.Client()))
	if err := client.SendMessage(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("send via fallback after incompatible h3 file: %v", err)
	}

	fallbackMu.Lock()
	if fallbackCalls != 1 {
		t.Fatalf("fallback calls = %d, want 1", fallbackCalls)
	}
	fallbackMu.Unlock()
}

func TestValidateRuntimeIrisBaseURL_TransportScheme(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		baseURL   string
		wantErr   bool
	}{
		{name: "h3 accepts https", transport: "h3", baseURL: "https://iris.example"},
		{name: "h3 rejects http", transport: "h3", baseURL: "http://iris.example", wantErr: true},
		{name: "http3 alias rejects http", transport: "http3", baseURL: "http://iris.example", wantErr: true},
		{name: "quic alias rejects http", transport: "quic", baseURL: "http://iris.example", wantErr: true},
		{name: "uppercase h3 alias rejects http", transport: "H3", baseURL: "http://iris.example", wantErr: true},
		{name: "http2 accepts https", transport: "http2", baseURL: "https://iris.example"},
		{name: "http2 rejects http", transport: "http2", baseURL: "http://iris.example", wantErr: true},
		{name: "h2 alias rejects http", transport: "h2", baseURL: "http://iris.example", wantErr: true},
		{name: "h2c accepts http", transport: "h2c", baseURL: "http://iris.example"},
		{name: "h2c rejects https", transport: "h2c", baseURL: "https://iris.example", wantErr: true},
		{name: "http1 accepts http", transport: "http1", baseURL: "http://iris.example"},
		{name: "unknown keeps generic validation", transport: "custom", baseURL: "http://iris.example"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("IRIS_TRANSPORT", tc.transport)
			_, err := validateRuntimeIrisBaseURL(tc.baseURL)
			if tc.wantErr && err == nil {
				t.Fatal("validateRuntimeIrisBaseURL() error = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateRuntimeIrisBaseURL() error = %v, want nil", err)
			}
		})
	}
}

func TestRuntimeIrisClient_SendMessageAccepted_ReturnsRequestID(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotRequest iris.ReplyRequest
	server := httptest.NewServer(h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("X-Iris-Signature") == "" {
			t.Fatal("missing iris signature")
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		if err := json.NewEncoder(w).Encode(iris.ReplyAcceptedResponse{
			Success:   true,
			Delivery:  "queued",
			RequestID: "reply-123",
			Room:      "room-1",
			Type:      "text",
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}), &http2.Server{}))
	defer server.Close()

	client := NewRuntimeIrisClient(server.URL, "bot-token", "", nil, iris.WithTransport("h2c"))
	resp, err := client.SendMessageAccepted(context.Background(), "room-1", "hello")
	if err != nil {
		t.Fatalf("send accepted: %v", err)
	}

	if gotPath != iris.PathReply {
		t.Fatalf("path = %q, want %q", gotPath, iris.PathReply)
	}
	if gotRequest.Type != "text" || gotRequest.Room != "room-1" || gotRequest.Data != "hello" {
		t.Fatalf("request = %+v, want text room-1 hello", gotRequest)
	}
	if resp == nil || resp.RequestID != "reply-123" || resp.Delivery != "queued" {
		t.Fatalf("response = %+v, want queued reply-123", resp)
	}
}
