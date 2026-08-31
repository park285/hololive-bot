package delivery

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net"
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

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/require"
)

const (
	testBotToken             = "bot-token"
	testSingleLabelHost      = "host"
	testIrisHost             = "iris.example"
	testIrisBaseURL          = "https://" + testIrisHost + ":3001"
	testIrisBaseURLWithSlash = testIrisBaseURL + "/"
	testIrisHTTPBaseURL      = "http://" + testIrisHost
	testBareIPBaseURL        = "https://100.100.1.5:3001"
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
			t.Fatal("httptest TLS server missing certificate")
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
	ctx := t.Context()
	first := newRuntimeIrisReplyCounter(t, httptest.NewTLSServer)
	second := newRuntimeIrisReplyCounter(t, httptest.NewTLSServer)
	baseURLFilePath := writeRuntimeIrisBaseURLFile(t, first.server.URL)

	t.Setenv(irisBaseURLAllowedHostsEnv, testBaseURLHost(t, first.server.URL)+","+testBaseURLHost(t, second.server.URL))

	client := NewRuntimeIrisClient(second.server.URL, testBotToken, baseURLFilePath, nil,
		iris.WithHTTPClient(tlsClientForServers(t, first.server, second.server)))

	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()

	if err := client.SendMessage(ctx, testRoomID, "hello"); err != nil {
		t.Fatalf("send via first override: %v", err)
	}

	assertRuntimeIrisReplyCalls(t, first, 1, "first calls after first send")

	if err := os.WriteFile(baseURLFilePath, []byte(second.server.URL), 0o600); err != nil {
		t.Fatalf("write second base url file: %v", err)
	}

	if err := client.SendMessage(ctx, testRoomID, "cached"); err != nil {
		t.Fatalf("send before resolve interval expiry: %v", err)
	}

	assertRuntimeIrisReplyCalls(t, first, 2, "first calls before resolve interval expiry")
	assertRuntimeIrisReplyCalls(t, second, 0, "second calls before resolve interval expiry")

	require.Eventually(t, func() bool {
		if err := client.SendMessage(ctx, testRoomID, "world"); err != nil {
			return false
		}

		return second.callCount() == 1
	}, 2*time.Second, 50*time.Millisecond)

	assertRuntimeIrisReplyCalls(t, second, 1, "second calls after reload")
}

func TestRuntimeIrisClient_SendMessagePreservesBaseURLFileDeploymentPrefix(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "http1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/tenant/iris/reply"; got != want {
			t.Fatalf("request path = %q, want %q", got, want)
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	baseURLFilePath := writeRuntimeIrisBaseURLFile(t, server.URL+"/tenant/iris///")
	client := NewRuntimeIrisClient(
		server.URL,
		testBotToken,
		baseURLFilePath,
		nil,
		iris.WithHTTPClient(server.Client()),
		iris.WithTransport("http1"),
	)

	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	if err := client.SendMessage(t.Context(), testRoomID, "hello"); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
}

func TestRuntimeIrisClientRejectsDialOutsideInitialBaseURLAllowset(t *testing.T) {
	client := NewRuntimeIrisClient("https://192.0.2.10:3001", testBotToken, "", nil)

	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	if err := client.allowH3Dial(t.Context(), net.ParseIP("192.0.2.10")); err != nil {
		t.Fatalf("h3DialGuard(allowed IP) error = %v", err)
	}

	if err := client.allowH3Dial(t.Context(), net.ParseIP("192.0.2.11")); err == nil {
		t.Fatal("h3DialGuard(disallowed IP) error = nil")
	}
}

func TestRuntimeIrisClientDialGuardFollowsRotatedBaseURL(t *testing.T) {
	dir := t.TempDir()
	baseURLFilePath := filepath.Join(dir, "iris_base_url")

	if err := os.WriteFile(baseURLFilePath, []byte("https://192.0.2.10:3001"), 0o600); err != nil {
		t.Fatalf("write initial base URL: %v", err)
	}

	client := NewRuntimeIrisClient("https://192.0.2.10:3001", testBotToken, baseURLFilePath, nil)

	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	if err := client.allowH3Dial(t.Context(), net.ParseIP("192.0.2.10")); err != nil {
		t.Fatalf("allowH3Dial(initial IP) error = %v", err)
	}

	if err := os.WriteFile(baseURLFilePath, []byte("https://192.0.2.11:3001"), 0o600); err != nil {
		t.Fatalf("write rotated base URL: %v", err)
	}

	if err := client.allowH3Dial(t.Context(), net.ParseIP("192.0.2.11")); err != nil {
		t.Fatalf("allowH3Dial(rotated IP) error = %v", err)
	}

	if err := client.allowH3Dial(t.Context(), net.ParseIP("192.0.2.10")); err == nil {
		t.Fatal("allowH3Dial(stale IP) error = nil")
	}
}

func TestRuntimeIrisClient_SendMessageDefaultsToReplyRetry(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "http1")

	ctx := t.Context()

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

	client := NewRuntimeIrisClient(server.URL, testBotToken, "", nil, iris.WithHTTPClient(server.Client()), iris.WithTransport("http1"))

	if err := client.SendMessage(ctx, testRoomID, "hello"); err != nil {
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
	for _, tc := range runtimeIrisBaseURLFileCases() {
		t.Run(tc.name, func(t *testing.T) {
			setRuntimeIrisBaseURLEnv(t, tc.env)

			baseURLFilePath := writeRuntimeIrisBaseURLFileCase(t, &tc)

			var logBuffer bytes.Buffer

			logger := slog.New(slog.NewTextHandler(&logBuffer, nil))
			client := NewRuntimeIrisClient("https://fallback.example", testBotToken, baseURLFilePath, logger)
			assertRuntimeIrisBaseURLResolve(t, client, &logBuffer, &tc)
		})
	}
}

func runtimeIrisBaseURLFileCases() []runtimeIrisBaseURLFileCase {
	cases := runtimeIrisBaseURLFileSchemeCases()

	cases = append(cases, runtimeIrisBaseURLFileHostCases()...)
	cases = append(cases, runtimeIrisBaseURLFileShapeCases()...)
	cases = append(cases, runtimeIrisBaseURLFileStatCases()...)

	return cases
}

func runtimeIrisBaseURLFileSchemeCases() []runtimeIrisBaseURLFileCase {
	return []runtimeIrisBaseURLFileCase{
		{
			name:             "accepts bare IP host when no allowlist is configured",
			fileContent:      testBareIPBaseURL,
			wantBaseURL:      testBareIPBaseURL,
			wantWarnContains: "host is unvalidated",
		},
		{
			name:            "rejects http bare IP host when no allowlist is configured",
			fileContent:     "http://100.100.1.5:3001",
			wantErrContains: "https",
		},
		{
			name:        "accepts https host without explicit port",
			fileContent: "https://" + testSingleLabelHost + "/",
			env:         map[string]string{irisH3ServerNameEnv: testSingleLabelHost},
			wantBaseURL: "https://" + testSingleLabelHost,
		},
	}
}

func runtimeIrisBaseURLFileHostCases() []runtimeIrisBaseURLFileCase {
	return []runtimeIrisBaseURLFileCase{
		{
			name:        "accepts bare IP host matching allowed hosts",
			fileContent: testBareIPBaseURL,
			env:         map[string]string{irisBaseURLAllowedHostsEnv: "100.100.1.5"},
			wantBaseURL: testBareIPBaseURL,
		},
		{
			name:        "accepts bare IP host matching trimmed allowed hosts",
			fileContent: testBareIPBaseURL,
			env:         map[string]string{irisBaseURLAllowedHostsEnv: " otherhost, 100.100.1.5 "},
			wantBaseURL: testBareIPBaseURL,
		},
		{
			name:            "rejects bare IP host mismatching allowed hosts",
			fileContent:     testBareIPBaseURL,
			env:             map[string]string{irisBaseURLAllowedHostsEnv: "otherhost"},
			wantErrContains: "host",
		},
		{
			name:            "rejects bare IP host mismatching configured H3 server name",
			fileContent:     testBareIPBaseURL,
			env:             map[string]string{irisH3ServerNameEnv: testIrisHost},
			wantErrContains: "host",
		},
		{
			name:            "rejects http attacker URL",
			fileContent:     "http://attacker.example:3001/",
			env:             map[string]string{irisH3ServerNameEnv: testIrisHost},
			wantErrContains: "host",
		},
		{
			name:            "rejects host mismatch against H3 server name",
			fileContent:     "https://attacker.example:3001/",
			env:             map[string]string{irisH3ServerNameEnv: testIrisHost},
			wantErrContains: "host",
		},
		{
			name:        "accepts matching H3 server name",
			fileContent: testIrisBaseURLWithSlash,
			env:         map[string]string{irisH3ServerNameEnv: testIrisHost},
			wantBaseURL: testIrisBaseURL,
		},
	}
}

func runtimeIrisBaseURLFileShapeCases() []runtimeIrisBaseURLFileCase {
	return []runtimeIrisBaseURLFileCase{
		{
			name:            "rejects nonnumeric explicit port",
			fileContent:     "https://" + testIrisHost + ":port/",
			env:             map[string]string{irisH3ServerNameEnv: testIrisHost},
			wantErrContains: "port",
		},
		{
			name:            "rejects userinfo",
			fileContent:     "https://token@" + testIrisHost + ":3001/",
			env:             map[string]string{irisH3ServerNameEnv: testIrisHost},
			wantErrContains: "userinfo",
		},
		{
			name:            "rejects path tricks",
			fileContent:     testIrisBaseURL + "/%2e%2e/admin",
			env:             map[string]string{irisH3ServerNameEnv: testIrisHost},
			wantErrContains: "path",
		},
	}
}

func runtimeIrisBaseURLFileStatCases() []runtimeIrisBaseURLFileCase {
	strictEnv := map[string]string{appEnvKey: appEnvProduction, irisH3ServerNameEnv: testIrisHost}
	skipStatEnv := map[string]string{
		appEnvKey:                        appEnvProduction,
		irisH3ServerNameEnv:              testIrisHost,
		irisBaseURLFileSkipStatChecksEnv: "true",
	}

	return []runtimeIrisBaseURLFileCase{
		{
			name:            "rejects symlink in production strict mode",
			fileContent:     testIrisBaseURLWithSlash,
			useSymlink:      true,
			env:             strictEnv,
			wantErrContains: "symlink",
		},
		{
			name:             "rejects symlink parent in production strict mode",
			fileContent:      testIrisBaseURLWithSlash,
			useSymlinkParent: true,
			env:              strictEnv,
			wantErrContains:  "parent",
		},
		{
			name:            "rejects world writable file in production strict mode",
			fileContent:     testIrisBaseURLWithSlash,
			fileMode:        0o666,
			env:             strictEnv,
			wantErrContains: "permission",
		},
		{
			name:        "accepts world writable file when stat checks are skipped",
			fileContent: testIrisBaseURLWithSlash,
			fileMode:    0o666,
			env:         skipStatEnv,
			wantBaseURL: testIrisBaseURL,
		},
		{
			name:             "accepts symlink parent when stat checks are skipped",
			fileContent:      testIrisBaseURLWithSlash,
			useSymlinkParent: true,
			env:              skipStatEnv,
			wantBaseURL:      testIrisBaseURL,
		},
		{
			name:            "uses fallback when file override path is empty",
			fileContent:     "https://attacker.example:3001/",
			disableFilePath: true,
			env:             map[string]string{irisH3ServerNameEnv: testIrisHost},
			wantBaseURL:     "https://fallback.example",
		},
	}
}

func setRuntimeIrisBaseURLEnv(t *testing.T, env map[string]string) {
	t.Helper()

	for _, key := range []string{
		appEnvKey,
		irisBaseURLAllowedHostsEnv,
		irisBaseURLFileSkipStatChecksEnv,
		irisH3ServerNameEnv,
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

	if err := os.WriteFile(filepath.Join(cleanTarget, "iris_base_url"), []byte(testIrisBaseURLWithSlash), 0o600); err != nil {
		t.Fatalf("write clean target base url: %v", err)
	}

	resolvedTarget := filepath.Join(realParent, "target")
	if err := os.MkdirAll(resolvedTarget, 0o750); err != nil {
		t.Fatalf("mkdir resolved target: %v", err)
	}

	if err := os.WriteFile(filepath.Join(resolvedTarget, "iris_base_url"), []byte(testIrisBaseURLWithSlash), 0o600); err != nil {
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
			wantBaseURL:    testIrisBaseURL,
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

	t.Setenv(appEnvKey, appEnvProduction)
	t.Setenv(irisH3ServerNameEnv, testIrisHost)
	t.Setenv(irisBaseURLFileSkipStatChecksEnv, skipStatChecks)

	client := NewRuntimeIrisClient("http://fallback.example", testBotToken, uncleanPath, nil)
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

type runtimeIrisReplyCounter struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  int
}

func newRuntimeIrisReplyCounter(t *testing.T, newServer func(http.Handler) *httptest.Server) *runtimeIrisReplyCounter {
	t.Helper()

	counter := &runtimeIrisReplyCounter{}

	counter.server = newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Errorf("reply server path = %q", r.URL.Path)
		}

		counter.mu.Lock()

		counter.calls++
		counter.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))

	t.Cleanup(counter.server.Close)

	return counter
}

func (c *runtimeIrisReplyCounter) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.calls
}

func assertRuntimeIrisReplyCalls(t *testing.T, counter *runtimeIrisReplyCounter, want int, label string) {
	t.Helper()

	if got := counter.callCount(); got != want {
		t.Fatalf("%s = %d, want %d", label, got, want)
	}
}

func writeRuntimeIrisBaseURLFile(t *testing.T, content string) string {
	t.Helper()

	baseURLFilePath := filepath.Join(t.TempDir(), "iris_base_url")
	if err := os.WriteFile(baseURLFilePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write base url file: %v", err)
	}

	return baseURLFilePath
}

func assertSendMessageFailsWithoutFallback(t *testing.T, counter *runtimeIrisReplyCounter, client *RuntimeIrisClient, failure string) {
	t.Helper()

	if err := client.SendMessage(t.Context(), testRoomID, "hello"); err == nil {
		t.Fatalf("send with %s base URL file error = nil, want error", failure)
	}

	assertRuntimeIrisReplyCalls(t, counter, 0, "fallback calls")
}

func TestRuntimeIrisClient_SendMessage_FailsWhenBaseURLFileMissing(t *testing.T) {
	t.Parallel()

	fallback := newRuntimeIrisReplyCounter(t, httptest.NewServer)
	client := NewRuntimeIrisClient(fallback.server.URL, testBotToken, filepath.Join(t.TempDir(), "missing"), nil, iris.WithHTTPClient(&http.Client{}))

	assertSendMessageFailsWithoutFallback(t, fallback, client, "missing")
}

func TestRuntimeIrisClient_SendMessage_FailsWhenBaseURLFileIsEmpty(t *testing.T) {
	t.Parallel()

	fallback := newRuntimeIrisReplyCounter(t, httptest.NewServer)
	client := NewRuntimeIrisClient(fallback.server.URL, testBotToken, writeRuntimeIrisBaseURLFile(t, " \n"), nil, iris.WithHTTPClient(&http.Client{}))

	assertSendMessageFailsWithoutFallback(t, fallback, client, "empty")
}

func TestRuntimeIrisClient_SendMessage_FailsWhenBaseURLFileIsInvalid(t *testing.T) {
	t.Parallel()

	fallback := newRuntimeIrisReplyCounter(t, httptest.NewServer)
	client := NewRuntimeIrisClient(fallback.server.URL, testBotToken, writeRuntimeIrisBaseURLFile(t, "http:// bad"), nil, iris.WithHTTPClient(&http.Client{}))

	assertSendMessageFailsWithoutFallback(t, fallback, client, "invalid")
}

func TestRuntimeIrisClient_SendMessage_FailsWhenH3BaseURLFileUsesHTTP(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "h3")

	fallback := newRuntimeIrisReplyCounter(t, httptest.NewServer)
	client := NewRuntimeIrisClient(fallback.server.URL, testBotToken, writeRuntimeIrisBaseURLFile(t, "http://stale-iris.example"), nil, iris.WithHTTPClient(fallback.server.Client()))

	assertSendMessageFailsWithoutFallback(t, fallback, client, "h3 http")
}

func TestValidateRuntimeIrisBaseURL_TransportSchemeAndHTTPS(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		baseURL   string
		wantErr   bool
	}{
		{name: "h3 accepts https", transport: "h3", baseURL: testIrisBaseURL},
		{name: "h3 rejects http", transport: "h3", baseURL: testIrisHTTPBaseURL, wantErr: true},
		{name: "http3 alias rejects http", transport: "http3", baseURL: testIrisHTTPBaseURL, wantErr: true},
		{name: "quic alias rejects http", transport: "quic", baseURL: testIrisHTTPBaseURL, wantErr: true},
		{name: "uppercase h3 alias rejects http", transport: "H3", baseURL: testIrisHTTPBaseURL, wantErr: true},
		{name: "http1 rejects remote https", transport: "http1", baseURL: testIrisBaseURL, wantErr: true},
		{name: "http1 loopback diagnostics accepts https", transport: "http1", baseURL: "https://127.0.0.1:3001"},
		{name: "unknown rejects https", transport: "custom", baseURL: testIrisBaseURL, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("IRIS_TRANSPORT", tc.transport)
			t.Setenv(irisH3ServerNameEnv, testIrisHost)
			t.Setenv(irisBaseURLAllowedHostsEnv, "iris.example,127.0.0.1")

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
	t.Setenv(irisBaseURLAllowedHostsEnv, "127.0.0.1")

	t.Run("default h3 rejects http fallback", func(t *testing.T) {
		t.Setenv("IRIS_TRANSPORT", "")

		if _, err := validateHTTPBaseURL("http://127.0.0.1:3001"); err == nil {
			t.Fatal("validateHTTPBaseURL() error = nil, want h3 http fallback rejection")
		}
	})
}

func TestRuntimeIrisClient_SendMessageAccepted_ReturnsRequestID(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "http1")

	var (
		gotPath    string
		gotRequest iris.ReplyRequest
	)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("X-Iris-Signature") == "" {
			t.Fatal("missing iris signature")
		}

		if err := jsonv2.UnmarshalRead(r.Body, &gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.WriteHeader(http.StatusAccepted)

		if err := jsonv2.MarshalWrite(w, iris.ReplyAcceptedResponse{
			Success:   true,
			Delivery:  "queued",
			RequestID: "reply-123",
			Room:      testRoomID,
			Type:      "text",
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))

	server.Start()

	defer server.Close()

	client := NewRuntimeIrisClient(server.URL, testBotToken, "", nil, iris.WithTransport("http1"))

	resp, err := client.SendMessageAccepted(t.Context(), testRoomID, "hello")
	if err != nil {
		t.Fatalf("send accepted: %v", err)
	}

	if gotPath != iris.PathReply {
		t.Fatalf("path = %q, want %q", gotPath, iris.PathReply)
	}

	if gotRequest.Type != "text" || gotRequest.Room != testRoomID || gotRequest.Data != "hello" {
		t.Fatalf("request = %+v, want text room-1 hello", gotRequest)
	}

	if resp == nil || resp.RequestID != "reply-123" || resp.Delivery != "queued" {
		t.Fatalf("response = %+v, want queued reply-123", resp)
	}
}

func TestRuntimeIrisClient_SendKaringHololive_ForwardsRequest(t *testing.T) {
	t.Setenv("IRIS_TRANSPORT", "http1")

	var (
		gotPath    string
		gotRequest iris.KaringHololiveRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get(iris.HeaderIrisSignature) == "" {
			t.Fatal("missing iris signature")
		}

		if err := jsonv2.UnmarshalRead(r.Body, &gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		streamCount := 1
		if err := jsonv2.MarshalWrite(w, iris.KaringDryRunResponse{
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
		testBotToken,
		"",
		nil,
		iris.WithBotControlToken("bot-control-secret"),
		iris.WithHTTPClient(server.Client()),
		iris.WithTransport("http1"),
	)

	resp, err := client.SendKaringHololive(t.Context(), iris.KaringHololiveRequest{
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
