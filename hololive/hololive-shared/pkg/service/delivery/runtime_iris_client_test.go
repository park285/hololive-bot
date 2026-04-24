package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/park285/iris-client-go/iris"
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
