package delivery

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/require"
)

func writeRuntimeIrisResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write iris response: %v", err)
	}
}

func tlsClientForServers(t *testing.T, servers ...*httptest.Server) *http.Client {
	t.Helper()

	roots := x509.NewCertPool()
	for _, server := range servers {
		if server == nil || server.TLS == nil || len(server.TLS.Certificates) == 0 || len(server.TLS.Certificates[0].Certificate) == 0 {
			t.Fatalf("httptest TLS server missing certificate")
		}
		cert, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
		if err != nil {
			t.Fatalf("parse httptest TLS certificate: %v", err)
		}
		roots.AddCert(cert)
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport type = %T, want *http.Transport", http.DefaultTransport)
	}
	transport := defaultTransport.Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport}
}

func TestRuntimeIrisClient_SendMessage_UsesBaseURLFileOverrideAndReloads(t *testing.T) {
	ctx := context.Background()
	botToken := "bot-token"
	var firstMu sync.Mutex
	firstCalls := 0
	first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("first server path = %q", r.URL.Path)
		}
		firstMu.Lock()
		firstCalls++
		firstMu.Unlock()
		w.WriteHeader(http.StatusOK)
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	defer first.Close()

	var secondMu sync.Mutex
	secondCalls := 0
	second := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("second server path = %q", r.URL.Path)
		}
		secondMu.Lock()
		secondCalls++
		secondMu.Unlock()
		w.WriteHeader(http.StatusOK)
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	defer second.Close()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte(first.URL), 0o600); err != nil {
		t.Fatalf("write first base url file: %v", err)
	}

	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", testBaseURLHost(t, first.URL)+","+testBaseURLHost(t, second.URL))

	client := NewRuntimeIrisClient(second.URL, botToken, baseURLFilePath, nil, iris.WithHTTPClient(tlsClientForServers(t, first, second)))
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

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

	if err := client.SendMessage(ctx, "room-1", "cached"); err != nil {
		t.Fatalf("send before resolve interval expiry: %v", err)
	}
	firstMu.Lock()
	if firstCalls != 2 {
		t.Fatalf("first calls before resolve interval expiry = %d, want 2", firstCalls)
	}
	firstMu.Unlock()
	secondMu.Lock()
	if secondCalls != 0 {
		t.Fatalf("second calls before resolve interval expiry = %d, want 0", secondCalls)
	}
	secondMu.Unlock()

	require.Eventually(t, func() bool {
		if err := client.SendMessage(ctx, "room-1", "world"); err != nil {
			return false
		}
		secondMu.Lock()
		defer secondMu.Unlock()
		return secondCalls == 1
	}, 2*time.Second, 50*time.Millisecond)

	secondMu.Lock()
	if secondCalls != 1 {
		t.Fatalf("second calls after reload = %d, want 1", secondCalls)
	}
	secondMu.Unlock()
}

func TestRuntimeIrisClient_SendMessageDefaultsToReplyRetry(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "http1")

	ctx := context.Background()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Fatalf("path = %q, want %q", r.URL.Path, iris.PathReply)
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			writeRuntimeIrisResponse(t, w, `{"error":"rate limited"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	defer server.Close()

	client := NewRuntimeIrisClient(server.URL, "bot-token", "", nil, iris.WithHTTPClient(server.Client()), iris.WithTransport("http1"))

	if err := client.SendMessage(ctx, "room-1", "hello"); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

type runtimeIrisBaseURLFileCase struct {
	name             string
	fileContent      string
	fileMode         os.FileMode
	useSymlink       bool
	useSymlinkParent bool
	disableFilePath  bool
	env              map[string]string
	wantBaseURL      string
	wantErrContains  string
	wantWarnContains string
}

func TestRuntimeIrisClient_ResolveBaseURLFileOverrideValidation(t *testing.T) {
	tests := []runtimeIrisBaseURLFileCase{
		{
			name:             "accepts bare IP host when no allowlist is configured",
			fileContent:      "https://100.100.1.5:3001",
			wantBaseURL:      "https://100.100.1.5:3001",
			wantWarnContains: "host is unvalidated",
		},
		{
			name:            "rejects http bare IP host when no allowlist is configured",
			fileContent:     "http://100.100.1.5:3001",
			wantErrContains: "https",
		},
		{
			name:        "accepts h2c loopback diagnostics http override",
			fileContent: "http://127.0.0.1:3001/",
			env: map[string]string{
				"IRIS_TRANSPORT":              "h2c",
				"IRIS_BASE_URL_ALLOWED_HOSTS": "127.0.0.1",
			},
			wantBaseURL: "http://127.0.0.1:3001",
		},
		{
			name:        "accepts https host without explicit port",
			fileContent: "https://host/",
			env:         map[string]string{"IRIS_H3_SERVER_NAME": "host"},
			wantBaseURL: "https://host",
		},
		{
			name:        "accepts bare IP host matching allowed hosts",
			fileContent: "https://100.100.1.5:3001",
			env:         map[string]string{"IRIS_BASE_URL_ALLOWED_HOSTS": "100.100.1.5"},
			wantBaseURL: "https://100.100.1.5:3001",
		},
		{
			name:        "accepts bare IP host matching trimmed allowed hosts",
			fileContent: "https://100.100.1.5:3001",
			env:         map[string]string{"IRIS_BASE_URL_ALLOWED_HOSTS": " otherhost, 100.100.1.5 "},
			wantBaseURL: "https://100.100.1.5:3001",
		},
		{
			name:            "rejects bare IP host mismatching allowed hosts",
			fileContent:     "https://100.100.1.5:3001",
			env:             map[string]string{"IRIS_BASE_URL_ALLOWED_HOSTS": "otherhost"},
			wantErrContains: "host",
		},
		{
			name:            "rejects bare IP host mismatching configured H3 server name",
			fileContent:     "https://100.100.1.5:3001",
			env:             map[string]string{"IRIS_H3_SERVER_NAME": "iris.example"},
			wantErrContains: "host",
		},
		{
			name:            "rejects http attacker URL",
			fileContent:     "http://attacker.example:3001/",
			env:             map[string]string{"IRIS_H3_SERVER_NAME": "iris.example"},
			wantErrContains: "host",
		},
		{
			name:            "rejects nonnumeric explicit port",
			fileContent:     "https://iris.example:port/",
			env:             map[string]string{"IRIS_H3_SERVER_NAME": "iris.example"},
			wantErrContains: "port",
		},
		{
			name:            "rejects host mismatch against H3 server name",
			fileContent:     "https://attacker.example:3001/",
			env:             map[string]string{"IRIS_H3_SERVER_NAME": "iris.example"},
			wantErrContains: "host",
		},
		{
			name:            "rejects userinfo",
			fileContent:     "https://token@iris.example:3001/",
			env:             map[string]string{"IRIS_H3_SERVER_NAME": "iris.example"},
			wantErrContains: "userinfo",
		},
		{
			name:            "rejects path tricks",
			fileContent:     "https://iris.example:3001/%2e%2e/admin",
			env:             map[string]string{"IRIS_H3_SERVER_NAME": "iris.example"},
			wantErrContains: "path",
		},
		{
			name:        "accepts matching H3 server name",
			fileContent: "https://iris.example:3001/",
			env:         map[string]string{"IRIS_H3_SERVER_NAME": "iris.example"},
			wantBaseURL: "https://iris.example:3001",
		},
		{
			name:            "rejects symlink in production strict mode",
			fileContent:     "https://iris.example:3001/",
			useSymlink:      true,
			env:             map[string]string{"APP_ENV": "production", "IRIS_H3_SERVER_NAME": "iris.example"},
			wantErrContains: "symlink",
		},
		{
			name:             "rejects symlink parent in production strict mode",
			fileContent:      "https://iris.example:3001/",
			useSymlinkParent: true,
			env:              map[string]string{"APP_ENV": "production", "IRIS_H3_SERVER_NAME": "iris.example"},
			wantErrContains:  "parent",
		},
		{
			name:            "rejects world writable file in production strict mode",
			fileContent:     "https://iris.example:3001/",
			fileMode:        0o666,
			env:             map[string]string{"APP_ENV": "production", "IRIS_H3_SERVER_NAME": "iris.example"},
			wantErrContains: "permission",
		},
		{
			name:        "accepts world writable file when stat checks are skipped",
			fileContent: "https://iris.example:3001/",
			fileMode:    0o666,
			env: map[string]string{
				"APP_ENV":                             "production",
				"IRIS_H3_SERVER_NAME":                 "iris.example",
				"IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS": "true",
			},
			wantBaseURL: "https://iris.example:3001",
		},
		{
			name:             "accepts symlink parent when stat checks are skipped",
			fileContent:      "https://iris.example:3001/",
			useSymlinkParent: true,
			env: map[string]string{
				"APP_ENV":                             "production",
				"IRIS_H3_SERVER_NAME":                 "iris.example",
				"IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS": "true",
			},
			wantBaseURL: "https://iris.example:3001",
		},
		{
			name:            "uses fallback when file override path is empty",
			fileContent:     "https://attacker.example:3001/",
			disableFilePath: true,
			env:             map[string]string{"IRIS_H3_SERVER_NAME": "iris.example"},
			wantBaseURL:     "https://fallback.example",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setRuntimeIrisBaseURLEnv(t, tc.env)
			baseURLFilePath := writeRuntimeIrisBaseURLFileCase(t, &tc)
			var logBuffer bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuffer, nil))
			client := NewRuntimeIrisClient("https://fallback.example", "bot-token", baseURLFilePath, logger)
			assertRuntimeIrisBaseURLResolve(t, client, &logBuffer, &tc)
		})
	}
}

func setRuntimeIrisBaseURLEnv(t *testing.T, env map[string]string) {
	t.Helper()

	for _, key := range []string{
		"APP_ENV",
		"IRIS_BASE_URL_ALLOWED_HOSTS",
		"IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS",
		"IRIS_H3_SERVER_NAME",
		"IRIS_TRANSPORT",
	} {
		t.Setenv(key, "")
	}
	for key, value := range env {
		t.Setenv(key, value)
	}
}

func writeRuntimeIrisBaseURLFileCase(t *testing.T, tc *runtimeIrisBaseURLFileCase) string {
	t.Helper()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if tc.useSymlinkParent {
		baseURLFilePath = writeRuntimeIrisBaseURLInSymlinkParent(t, dir, tc.fileContent)
	} else if err := os.WriteFile(baseURLFilePath, []byte(tc.fileContent), 0o600); err != nil {
		t.Fatalf("write base url file: %v", err)
	}
	if tc.fileMode != 0 {
		if err := os.Chmod(baseURLFilePath, tc.fileMode); err != nil {
			t.Fatalf("chmod base url file: %v", err)
		}
	}
	if tc.useSymlink {
		targetPath := baseURLFilePath
		baseURLFilePath = filepath.Join(dir, "iris_base_url_link")
		if err := os.Symlink(targetPath, baseURLFilePath); err != nil {
			t.Fatalf("symlink base url file: %v", err)
		}
	}
	if tc.disableFilePath {
		return ""
	}
	return baseURLFilePath
}

func writeRuntimeIrisBaseURLInSymlinkParent(t *testing.T, dir, content string) string {
	t.Helper()

	realParent := filepath.Join(dir, "real-parent")
	if err := os.Mkdir(realParent, 0o750); err != nil {
		t.Fatalf("mkdir real parent: %v", err)
	}
	linkParent := filepath.Join(dir, "link-parent")
	if err := os.Symlink(realParent, linkParent); err != nil {
		t.Fatalf("symlink parent: %v", err)
	}
	baseURLFilePath := filepath.Join(linkParent, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write base url file: %v", err)
	}
	return baseURLFilePath
}

func assertRuntimeIrisBaseURLResolve(t *testing.T, client *RuntimeIrisClient, logBuffer *bytes.Buffer, tc *runtimeIrisBaseURLFileCase) {
	t.Helper()

	got, err := client.resolver.resolve()
	if tc.wantErrContains != "" {
		assertRuntimeIrisBaseURLResolveError(t, err, tc.wantErrContains)
		return
	}
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if got != tc.wantBaseURL {
		t.Fatalf("resolve() = %q, want %q", got, tc.wantBaseURL)
	}
	assertRuntimeIrisBaseURLWarning(t, client, logBuffer, tc)
}

func assertRuntimeIrisBaseURLResolveError(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("resolve() error = nil, want containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("resolve() error = %v, want containing %q", err, want)
	}
}

func assertRuntimeIrisBaseURLWarning(t *testing.T, client *RuntimeIrisClient, logBuffer *bytes.Buffer, tc *runtimeIrisBaseURLFileCase) {
	t.Helper()

	if tc.wantWarnContains == "" {
		if strings.Contains(logBuffer.String(), "host is unvalidated") {
			t.Fatalf("unexpected unvalidated host warning: %s", logBuffer.String())
		}
		return
	}
	got, err := client.resolver.resolve()
	if err != nil {
		t.Fatalf("second resolve() error = %v, want nil", err)
	}
	if got != tc.wantBaseURL {
		t.Fatalf("second resolve() = %q, want %q", got, tc.wantBaseURL)
	}
	logs := logBuffer.String()
	if strings.Count(logs, tc.wantWarnContains) != 1 {
		t.Fatalf("warning count for %q in logs = %d, want 1; logs: %s", tc.wantWarnContains, strings.Count(logs, tc.wantWarnContains), logs)
	}
}

func TestRuntimeIrisClient_ResolveBaseURLFileRejectsUncleanSymlinkTraversalInProductionStrict(t *testing.T) {
	dir := t.TempDir()
	realParent := filepath.Join(dir, "real-parent")
	realChild := filepath.Join(realParent, "child")
	if err := os.MkdirAll(realChild, 0o750); err != nil {
		t.Fatalf("mkdir real child: %v", err)
	}
	linkParent := filepath.Join(dir, "symlink")
	if err := os.Symlink(realChild, linkParent); err != nil {
		t.Fatalf("symlink parent: %v", err)
	}

	cleanTarget := filepath.Join(dir, "target")
	if err := os.MkdirAll(cleanTarget, 0o750); err != nil {
		t.Fatalf("mkdir clean target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cleanTarget, "iris_base_url"), []byte("https://iris.example:3001/"), 0o600); err != nil {
		t.Fatalf("write clean target base url: %v", err)
	}

	resolvedTarget := filepath.Join(realParent, "target")
	if err := os.MkdirAll(resolvedTarget, 0o750); err != nil {
		t.Fatalf("mkdir resolved target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resolvedTarget, "iris_base_url"), []byte("https://iris.example:3001/"), 0o600); err != nil {
		t.Fatalf("write resolved target base url: %v", err)
	}

	uncleanPath := strings.Join([]string{linkParent, "..", "target", "iris_base_url"}, string(os.PathSeparator))
	tests := []struct {
		name            string
		skipStatChecks  string
		wantBaseURL     string
		wantErrContains string
	}{
		{
			name:            "strict rejects unclean symlink traversal",
			wantErrContains: "clean",
		},
		{
			name:           "skip stat accepts normalized path",
			skipStatChecks: "true",
			wantBaseURL:    "https://iris.example:3001",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertUncleanSymlinkTraversalResolution(t, uncleanPath, tc.skipStatChecks, tc.wantBaseURL, tc.wantErrContains)
		})
	}
}

func assertUncleanSymlinkTraversalResolution(t *testing.T, uncleanPath, skipStatChecks, wantBaseURL, wantErrContains string) {
	t.Helper()

	t.Setenv("APP_ENV", "production")
	t.Setenv("IRIS_H3_SERVER_NAME", "iris.example")
	t.Setenv("IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS", skipStatChecks)

	client := NewRuntimeIrisClient("http://fallback.example", "bot-token", uncleanPath, nil)
	got, err := client.resolver.resolve()
	if wantErrContains != "" {
		assertRuntimeIrisBaseURLResolveError(t, err, wantErrContains)
		return
	}
	if err != nil {
		t.Fatalf("resolve() error = %v, want nil", err)
	}
	if got != wantBaseURL {
		t.Fatalf("resolve() = %q, want %q", got, wantBaseURL)
	}
}

func TestRuntimeIrisClient_SendMessage_FailsWhenBaseURLFileMissing(t *testing.T) {
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
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	defer fallback.Close()

	client := NewRuntimeIrisClient(fallback.URL, botToken, filepath.Join(t.TempDir(), "missing"), nil, iris.WithHTTPClient(&http.Client{}))
	if err := client.SendMessage(ctx, "room-1", "hello"); err == nil {
		t.Fatal("send with missing base URL file error = nil, want error")
	}

	fallbackMu.Lock()
	if fallbackCalls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fallbackCalls)
	}
	fallbackMu.Unlock()
}

func TestRuntimeIrisClient_SendMessage_FailsWhenBaseURLFileIsEmpty(t *testing.T) {
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
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	defer fallback.Close()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte(" \n"), 0o600); err != nil {
		t.Fatalf("write empty base url file: %v", err)
	}

	client := NewRuntimeIrisClient(fallback.URL, botToken, baseURLFilePath, nil, iris.WithHTTPClient(&http.Client{}))
	if err := client.SendMessage(ctx, "room-1", "hello"); err == nil {
		t.Fatal("send with empty base URL file error = nil, want error")
	}

	fallbackMu.Lock()
	if fallbackCalls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fallbackCalls)
	}
	fallbackMu.Unlock()
}

func TestRuntimeIrisClient_SendMessage_FailsWhenBaseURLFileIsInvalid(t *testing.T) {
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
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	defer fallback.Close()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte("http:// bad"), 0o600); err != nil {
		t.Fatalf("write invalid base url file: %v", err)
	}

	client := NewRuntimeIrisClient(fallback.URL, botToken, baseURLFilePath, nil, iris.WithHTTPClient(&http.Client{}))
	if err := client.SendMessage(ctx, "room-1", "hello"); err == nil {
		t.Fatal("send with invalid base URL file error = nil, want error")
	}

	fallbackMu.Lock()
	if fallbackCalls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fallbackCalls)
	}
	fallbackMu.Unlock()
}

func TestRuntimeIrisClient_SendMessage_FailsWhenH3BaseURLFileUsesHTTP(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "h3")

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
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	defer fallback.Close()

	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte("http://stale-iris.example"), 0o600); err != nil {
		t.Fatalf("write stale base url file: %v", err)
	}

	client := NewRuntimeIrisClient(fallback.URL, botToken, baseURLFilePath, nil, iris.WithHTTPClient(fallback.Client()))
	if err := client.SendMessage(ctx, "room-1", "hello"); err == nil {
		t.Fatal("send with h3 http base URL file error = nil, want error")
	}

	fallbackMu.Lock()
	if fallbackCalls != 0 {
		t.Fatalf("fallback calls = %d, want 0", fallbackCalls)
	}
	fallbackMu.Unlock()
}

func TestValidateRuntimeIrisBaseURL_TransportSchemeAndHTTPS(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		baseURL   string
		wantErr   bool
	}{
		{name: "h3 accepts https", transport: "h3", baseURL: "https://iris.example:3001"},
		{name: "h3 rejects http", transport: "h3", baseURL: "http://iris.example", wantErr: true},
		{name: "http3 alias rejects http", transport: "http3", baseURL: "http://iris.example", wantErr: true},
		{name: "quic alias rejects http", transport: "quic", baseURL: "http://iris.example", wantErr: true},
		{name: "uppercase h3 alias rejects http", transport: "H3", baseURL: "http://iris.example", wantErr: true},
		{name: "http2 rejects remote https", transport: "http2", baseURL: "https://iris.example:3001", wantErr: true},
		{name: "http2 loopback diagnostics accepts https", transport: "http2", baseURL: "https://127.0.0.1:3001"},
		{name: "http2 rejects remote http", transport: "http2", baseURL: "http://iris.example", wantErr: true},
		{name: "h2 alias rejects http", transport: "h2", baseURL: "http://iris.example", wantErr: true},
		{name: "h2c rejects remote http", transport: "h2c", baseURL: "http://iris.example", wantErr: true},
		{name: "h2c loopback diagnostics accepts http", transport: "h2c", baseURL: "http://127.0.0.1:3001"},
		{name: "h2c loopback diagnostics rejects https", transport: "h2c", baseURL: "https://127.0.0.1:3001", wantErr: true},
		{name: "h2c rejects remote https", transport: "h2c", baseURL: "https://iris.example:3001", wantErr: true},
		{name: "http1 rejects remote https", transport: "http1", baseURL: "https://iris.example:3001", wantErr: true},
		{name: "http1 loopback diagnostics accepts https", transport: "http1", baseURL: "https://127.0.0.1:3001"},
		{name: "unknown rejects https", transport: "custom", baseURL: "https://iris.example:3001", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("IRIS_TRANSPORT", tc.transport)
			t.Setenv("IRIS_H3_SERVER_NAME", "iris.example")
			t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", "iris.example,127.0.0.1")
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

func TestValidateHTTPBaseURL_TransportScheme(t *testing.T) {
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", "127.0.0.1")

	t.Run("default h3 rejects http fallback", func(t *testing.T) {
		t.Setenv("IRIS_TRANSPORT", "")
		if _, err := validateHTTPBaseURL("http://127.0.0.1:3001"); err == nil {
			t.Fatal("validateHTTPBaseURL() error = nil, want h3 http fallback rejection")
		}
	})

	t.Run("h2c loopback diagnostics accepts http fallback", func(t *testing.T) {
		t.Setenv("IRIS_TRANSPORT", "h2c")
		if _, err := validateHTTPBaseURL("http://127.0.0.1:3001"); err != nil {
			t.Fatalf("validateHTTPBaseURL() error = %v, want nil", err)
		}
	})

	t.Run("h2c loopback diagnostics rejects https fallback", func(t *testing.T) {
		t.Setenv("IRIS_TRANSPORT", "h2c")
		if _, err := validateHTTPBaseURL("https://127.0.0.1:3001"); err == nil {
			t.Fatal("validateHTTPBaseURL() error = nil, want h2c https fallback rejection")
		}
	})
}

func TestRuntimeIrisClient_SendMessageAccepted_ReturnsRequestID(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "h2c")

	var gotPath string
	var gotRequest iris.ReplyRequest
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	if server.Config.Protocols == nil {
		server.Config.Protocols = new(http.Protocols)
	}
	server.Config.Protocols.SetHTTP1(true)
	server.Config.Protocols.SetUnencryptedHTTP2(true)
	server.Start()
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

func TestRuntimeIrisClient_SendKaringHololive_ForwardsRequest(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "http1")

	var gotPath string
	var gotRequest iris.KaringHololiveRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get(iris.HeaderIrisSignature) == "" {
			t.Fatal("missing iris signature")
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		streamCount := 1
		if err := json.NewEncoder(w).Encode(iris.KaringDryRunResponse{
			OK:          true,
			DryRun:      true,
			TemplateID:  133220,
			StreamCount: &streamCount,
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewRuntimeIrisClient(
		server.URL,
		"bot-token",
		"",
		nil,
		iris.WithBotControlToken("bot-control-secret"),
		iris.WithHTTPClient(server.Client()),
		iris.WithTransport("http1"),
	)
	resp, err := client.SendKaringHololive(context.Background(), iris.KaringHololiveRequest{
		Streams: []iris.KaringContentItem{{
			Title:  "test stream",
			URL:    "https://www.youtube.com/watch?v=video000001",
			Status: iris.KaringStreamStatusUpcoming,
		}},
		ExtraArgs: iris.KaringTemplateArgs{"time_left": "10 minutes"},
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("SendKaringHololive() error = %v", err)
	}

	if gotPath != iris.PathKaringHololive {
		t.Fatalf("path = %q, want %q", gotPath, iris.PathKaringHololive)
	}
	if len(gotRequest.Streams) != 1 || gotRequest.Streams[0].Status != iris.KaringStreamStatusUpcoming {
		t.Fatalf("Streams = %+v", gotRequest.Streams)
	}
	if gotRequest.ExtraArgs["time_left"] != "10 minutes" {
		t.Fatalf("ExtraArgs[time_left] = %q, want 10 minutes", gotRequest.ExtraArgs["time_left"])
	}
	if resp == nil || !resp.OK || resp.StreamCount == nil || *resp.StreamCount != 1 {
		t.Fatalf("response = %+v, want stream count 1", resp)
	}
}

func testBaseURLHost(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test base URL: %v", err)
	}
	return parsed.Hostname()
}
