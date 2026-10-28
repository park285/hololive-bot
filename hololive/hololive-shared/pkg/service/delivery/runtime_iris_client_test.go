package delivery

import (
	"crypto/tls"
	"crypto/x509"
	jsonv2 "encoding/json/v2"
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

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

const (
	testBotToken             = "bot-token"
	testSingleLabelHost      = "host"
	testIrisHost             = "iris.example"
	testIrisBaseURL          = "https://" + testIrisHost + ":3001"
	testIrisBaseURLWithSlash = testIrisBaseURL + "/"
	testBareIPBaseURL        = "https://100.100.1.5:3001"
)

func newTestRuntimeIrisClient(t *testing.T, config settings.IrisConfig, opts ...iris.ClientOption) *RuntimeIrisClient {
	t.Helper()

	if config.BotToken == "" {
		config.BotToken = testBotToken
	}

	client, err := NewRuntimeIrisClient(t.Context(), config, nil, opts...)
	require.NoError(t, err)

	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return client
}

func writeRuntimeIrisResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()

	if _, err := w.Write([]byte(body)); err != nil {
		t.Errorf("write iris response: %v", err)
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

// expireRuntimeIrisSnapshot은 해석 snapshot 만료를 기다리지 않고 다음 호출이 refresh하게 한다.
func expireRuntimeIrisSnapshot(client *RuntimeIrisClient) {
	client.mu.Lock()
	defer client.mu.Unlock()

	client.resolveValidUntil = time.Time{}
}

func currentRuntimeIrisBinding(client *RuntimeIrisClient) *runtimeIrisBinding {
	client.mu.Lock()
	defer client.mu.Unlock()

	return client.current
}

func runtimeIrisBindingRefs(client *RuntimeIrisClient, binding *runtimeIrisBinding) int {
	client.mu.Lock()
	defer client.mu.Unlock()

	return binding.refs
}

func TestRuntimeIrisClientUsesBaseURLFileOverrideAndReloads(t *testing.T) {
	ctx := t.Context()
	first := newRuntimeIrisReplyCounter(t, httptest.NewTLSServer)
	second := newRuntimeIrisReplyCounter(t, httptest.NewTLSServer)
	baseURLFilePath := writeRuntimeIrisBaseURLFile(t, first.server.URL)

	client := newTestRuntimeIrisClient(t, settings.IrisConfig{
		BaseURL:             second.server.URL,
		BaseURLFile:         baseURLFilePath,
		BaseURLAllowedHosts: []string{testBaseURLHost(t, first.server.URL), testBaseURLHost(t, second.server.URL)},
	}, iris.WithHTTPClient(tlsClientForServers(t, first.server, second.server)))

	_, err := client.SendMessageAccepted(ctx, testRoomID, "hello")
	require.NoError(t, err)
	assertRuntimeIrisReplyCalls(t, first, 1, "first calls after first send")

	require.NoError(t, os.WriteFile(baseURLFilePath, []byte(second.server.URL), 0o600))

	_, err = client.SendMessageAccepted(ctx, testRoomID, "cached")
	require.NoError(t, err)
	assertRuntimeIrisReplyCalls(t, first, 2, "first calls before resolve interval expiry")
	assertRuntimeIrisReplyCalls(t, second, 0, "second calls before resolve interval expiry")

	require.Eventually(t, func() bool {
		if _, err := client.SendMessageAccepted(ctx, testRoomID, "world"); err != nil {
			return false
		}

		return second.callCount() == 1
	}, 2*time.Second, 50*time.Millisecond)
}

func TestRuntimeIrisClientPreservesBaseURLFileDeploymentPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/tenant/iris/reply"; got != want {
			t.Errorf("request path = %q, want %q", got, want)
		}

		w.WriteHeader(http.StatusOK)
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)

	client := newTestRuntimeIrisClient(t, settings.IrisConfig{
		BaseURL:             server.URL,
		BaseURLFile:         writeRuntimeIrisBaseURLFile(t, server.URL+"/tenant/iris///"),
		BaseURLAllowedHosts: []string{testBaseURLHost(t, server.URL)},
		Transport:           iris.TransportHTTP1,
	}, iris.WithHTTPClient(server.Client()))

	_, err := client.SendMessageAccepted(t.Context(), testRoomID, "hello")
	require.NoError(t, err)
}

func TestRuntimeIrisClientRejectsDialOutsideInitialBaseURLAllowset(t *testing.T) {
	client := newTestRuntimeIrisClient(t, settings.IrisConfig{BaseURL: "https://192.0.2.10:3001"}, iris.WithHTTPClient(&http.Client{}))

	require.NoError(t, client.h3DialGuard.allow(t.Context(), net.ParseIP("192.0.2.10")))
	require.Error(t, client.h3DialGuard.allow(t.Context(), net.ParseIP("192.0.2.11")))
}

func TestRuntimeIrisClientDialGuardFollowsRotatedBaseURL(t *testing.T) {
	baseURLFilePath := writeRuntimeIrisBaseURLFile(t, "https://192.0.2.10:3001")
	client := newTestRuntimeIrisClient(t, settings.IrisConfig{
		BaseURL:             "https://192.0.2.10:3001",
		BaseURLFile:         baseURLFilePath,
		BaseURLAllowedHosts: []string{"192.0.2.10", "192.0.2.11"},
	}, iris.WithHTTPClient(&http.Client{}))

	require.NoError(t, client.h3DialGuard.allow(t.Context(), net.ParseIP("192.0.2.10")))
	require.NoError(t, os.WriteFile(baseURLFilePath, []byte("https://192.0.2.11:3001"), 0o600))
	require.NoError(t, client.h3DialGuard.allow(t.Context(), net.ParseIP("192.0.2.11")))
	require.Error(t, client.h3DialGuard.allow(t.Context(), net.ParseIP("192.0.2.10")))
}

func TestRuntimeIrisClientDefaultsToReplyRetry(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iris.PathReply {
			t.Errorf("path = %q, want %q", r.URL.Path, iris.PathReply)
		}

		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			writeRuntimeIrisResponse(t, w, `{"error":"rate limited"}`)

			return
		}

		w.WriteHeader(http.StatusOK)
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)

	client := newTestRuntimeIrisClient(t, settings.IrisConfig{BaseURL: server.URL, Transport: iris.TransportHTTP1}, iris.WithHTTPClient(server.Client()))

	_, err := client.SendMessageAccepted(t.Context(), testRoomID, "hello", iris.WithClientRequestID("hololive-retry-1"))
	require.NoError(t, err)
	require.Equal(t, int32(2), attempts.Load())
}

// 회전으로 교체된 client는 진행 중인 호출이 끝날 때까지 참조를 유지하고, 마지막 release에서만 닫힌다.
func TestRuntimeIrisClientKeepsRotatedClientUntilInFlightCallReleases(t *testing.T) {
	ctx := t.Context()
	entered := make(chan struct{})
	unblock := make(chan struct{})

	first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-unblock
		w.WriteHeader(http.StatusOK)
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	t.Cleanup(first.Close)

	second := newRuntimeIrisReplyCounter(t, httptest.NewTLSServer)
	baseURLFilePath := writeRuntimeIrisBaseURLFile(t, first.URL)
	client := newTestRuntimeIrisClient(t, settings.IrisConfig{
		BaseURLFile:         baseURLFilePath,
		BaseURLAllowedHosts: []string{testBaseURLHost(t, first.URL), testBaseURLHost(t, second.server.URL)},
	}, iris.WithHTTPClient(tlsClientForServers(t, first, second.server)))

	rotatedOut := currentRuntimeIrisBinding(client)

	inFlight := make(chan error, 1)

	go func() {
		_, err := client.SendMessageAccepted(ctx, testRoomID, "slow")
		inFlight <- err
	}()

	<-entered

	require.NoError(t, os.WriteFile(baseURLFilePath, []byte(second.server.URL), 0o600))
	expireRuntimeIrisSnapshot(client)

	_, err := client.SendMessageAccepted(ctx, testRoomID, "rotated")
	require.NoError(t, err)
	assertRuntimeIrisReplyCalls(t, second, 1, "second calls after rotation")

	require.Equal(t, 1, runtimeIrisBindingRefs(client, rotatedOut), "rotated client must stay referenced by the in-flight call")

	close(unblock)
	require.NoError(t, <-inFlight)

	require.Zero(t, runtimeIrisBindingRefs(client, rotatedOut), "last release must drop the rotated client")
}

// refresh는 호출 goroutine에서 실행하므로 resolver panic이 호출자에게 그대로 전파되고, 다음 호출을 막지 않는다.
func TestRuntimeIrisClientPropagatesResolverPanic(t *testing.T) {
	server := newRuntimeIrisReplyCounter(t, httptest.NewServer)
	client := newTestRuntimeIrisClient(t, settings.IrisConfig{BaseURL: server.server.URL}, iris.WithHTTPClient(server.server.Client()))
	resolve := client.resolveBaseURL

	client.resolveBaseURL = func() (string, error) { panic("resolver boom") }

	expireRuntimeIrisSnapshot(client)
	require.PanicsWithValue(t, "resolver boom", func() {
		if _, err := client.SendMessageAccepted(t.Context(), testRoomID, "panic"); err != nil {
			t.Errorf("SendMessageAccepted() error = %v, want resolver panic", err)
		}
	})

	client.resolveBaseURL = resolve

	_, err := client.SendMessageAccepted(t.Context(), testRoomID, "after panic")
	require.NoError(t, err)
	assertRuntimeIrisReplyCalls(t, server, 1, "calls after panic")
}

func TestRuntimeIrisClientRejectsCallsAfterClose(t *testing.T) {
	server := newRuntimeIrisReplyCounter(t, httptest.NewServer)

	client, err := NewRuntimeIrisClient(t.Context(), settings.IrisConfig{BaseURL: server.server.URL, BotToken: testBotToken}, nil, iris.WithHTTPClient(server.server.Client()))
	require.NoError(t, err)

	_, err = client.SendMessageAccepted(t.Context(), testRoomID, "hello")
	require.NoError(t, err)
	require.NoError(t, client.Close())

	_, err = client.SendMessageAccepted(t.Context(), testRoomID, "world")
	require.ErrorIs(t, err, errRuntimeIrisClientClosed)
}

func TestNewRuntimeIrisClientRejectsInvalidConfigAtConstruction(t *testing.T) {
	cases := []struct {
		name   string
		config settings.IrisConfig
		opts   []iris.ClientOption
		want   string
	}{
		{name: "empty bot token", config: settings.IrisConfig{BaseURL: testIrisBaseURL}, want: "bot token"},
		{name: "no base URL source", config: settings.IrisConfig{BotToken: testBotToken}, want: "IRIS_BASE_URL"},
		{
			name:   "base URL file without allowlist",
			config: settings.IrisConfig{BotToken: testBotToken, BaseURLFile: "/run/iris_base_url"},
			want:   "IRIS_BASE_URL_FILE requires",
		},
		{
			name:   "missing base URL file",
			config: settings.IrisConfig{BotToken: testBotToken, BaseURLFile: filepath.Join(t.TempDir(), "missing-iris-base-url"), H3ServerName: testIrisHost},
			opts:   []iris.ClientOption{iris.WithHTTPClient(&http.Client{})},
			want:   "IRIS_BASE_URL_FILE",
		},
		{
			name:   "h3 rejects http base URL",
			config: settings.IrisConfig{BotToken: testBotToken, BaseURL: "http://" + testIrisHost, Transport: iris.TransportH3},
			want:   "https",
		},
		{
			name:   "h3 requires CA file",
			config: settings.IrisConfig{BotToken: testBotToken, BaseURL: testIrisBaseURL, Transport: iris.TransportH3},
			want:   "CA",
		},
		{
			name:   "http1 rejects remote host",
			config: settings.IrisConfig{BotToken: testBotToken, BaseURL: testIrisBaseURL, Transport: iris.TransportHTTP1},
			want:   "loopback",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewRuntimeIrisClient(t.Context(), tc.config, nil, tc.opts...)
			require.Nil(t, client)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

type runtimeIrisBaseURLFileCase struct {
	name             string
	fileContent      string
	fileMode         os.FileMode
	useSymlink       bool
	useSymlinkParent bool
	serverName       string
	allowedHosts     []string
	validateFileStat bool
	wantBaseURL      string
	wantErrContains  string
}

func TestRuntimeIrisBaseURLResolverValidatesFileOverride(t *testing.T) {
	for _, tc := range runtimeIrisBaseURLFileCases() {
		t.Run(tc.name, func(t *testing.T) {
			resolver, err := newRuntimeIrisBaseURLResolver(settings.IrisConfig{
				BaseURL:                 "https://fallback.example",
				BaseURLFile:             writeRuntimeIrisBaseURLFileCase(t, &tc),
				H3ServerName:            tc.serverName,
				BaseURLAllowedHosts:     tc.allowedHosts,
				ValidateBaseURLFileStat: tc.validateFileStat,
			})
			require.NoError(t, err)

			got, err := resolver.resolve()

			if tc.wantErrContains != "" {
				require.ErrorContains(t, err, tc.wantErrContains)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantBaseURL, got)
		})
	}
}

func runtimeIrisBaseURLFileCases() []runtimeIrisBaseURLFileCase {
	return append([]runtimeIrisBaseURLFileCase{
		{
			name:        "accepts https host without explicit port",
			fileContent: "https://" + testSingleLabelHost + "/",
			serverName:  testSingleLabelHost,
			wantBaseURL: "https://" + testSingleLabelHost,
		},
		{
			name:         "accepts bare IP host matching allowed hosts",
			fileContent:  testBareIPBaseURL,
			allowedHosts: []string{"otherhost", "100.100.1.5"},
			wantBaseURL:  testBareIPBaseURL,
		},
		{
			name:            "rejects bare IP host mismatching allowed hosts",
			fileContent:     testBareIPBaseURL,
			allowedHosts:    []string{"otherhost"},
			wantErrContains: "host",
		},
		{
			name:            "rejects bare IP host mismatching configured H3 server name",
			fileContent:     testBareIPBaseURL,
			serverName:      testIrisHost,
			wantErrContains: "host",
		},
		{
			name:            "rejects host mismatch against H3 server name",
			fileContent:     "https://attacker.example:3001/",
			serverName:      testIrisHost,
			wantErrContains: "host",
		},
		{
			name:        "accepts matching H3 server name",
			fileContent: testIrisBaseURLWithSlash,
			serverName:  testIrisHost,
			wantBaseURL: testIrisBaseURL,
		},
		{
			name:            "rejects nonnumeric explicit port",
			fileContent:     "https://" + testIrisHost + ":port/",
			serverName:      testIrisHost,
			wantErrContains: "port",
		},
		{
			name:            "rejects userinfo",
			fileContent:     "https://token@" + testIrisHost + ":3001/",
			serverName:      testIrisHost,
			wantErrContains: "userinfo",
		},
		{
			name:            "rejects path tricks",
			fileContent:     testIrisBaseURL + "/%2e%2e/admin",
			serverName:      testIrisHost,
			wantErrContains: "path",
		},
		{
			name:            "rejects empty file",
			fileContent:     " \n",
			serverName:      testIrisHost,
			wantErrContains: "IRIS_BASE_URL_FILE",
		},
	}, runtimeIrisBaseURLFileStatCases()...)
}

func runtimeIrisBaseURLFileStatCases() []runtimeIrisBaseURLFileCase {
	return []runtimeIrisBaseURLFileCase{
		{
			name:             "rejects symlink in production strict mode",
			fileContent:      testIrisBaseURLWithSlash,
			useSymlink:       true,
			serverName:       testIrisHost,
			validateFileStat: true,
			wantErrContains:  "symlink",
		},
		{
			name:             "rejects symlink parent in production strict mode",
			fileContent:      testIrisBaseURLWithSlash,
			useSymlinkParent: true,
			serverName:       testIrisHost,
			validateFileStat: true,
			wantErrContains:  "parent",
		},
		{
			name:             "rejects world writable file in production strict mode",
			fileContent:      testIrisBaseURLWithSlash,
			fileMode:         0o666,
			serverName:       testIrisHost,
			validateFileStat: true,
			wantErrContains:  "permission",
		},
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

func TestRuntimeIrisBaseURLResolverRejectsUncleanSymlinkTraversalInProductionStrict(t *testing.T) {
	dir := t.TempDir()
	realParent := filepath.Join(dir, "real-parent")
	realChild := filepath.Join(realParent, "child")

	require.NoError(t, os.MkdirAll(realChild, 0o750))

	linkParent := filepath.Join(dir, "symlink")
	require.NoError(t, os.Symlink(realChild, linkParent))

	for _, target := range []string{filepath.Join(dir, "target"), filepath.Join(realParent, "target")} {
		require.NoError(t, os.MkdirAll(target, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(target, "iris_base_url"), []byte(testIrisBaseURLWithSlash), 0o600))
	}

	// production은 stat 검사를 건너뛸 수 없어 정리되지 않은 symlink 경로를 항상 거절한다.
	resolver, err := newRuntimeIrisBaseURLResolver(settings.IrisConfig{
		BaseURLFile:             strings.Join([]string{linkParent, "..", "target", "iris_base_url"}, string(os.PathSeparator)),
		H3ServerName:            testIrisHost,
		ValidateBaseURLFileStat: true,
	})
	require.NoError(t, err)

	_, err = resolver.resolve()
	require.ErrorContains(t, err, "clean")
}

// resolver는 생성 시 받은 정책만 쓰므로 실행 중 env를 바꿔도 허용 집합이 달라지지 않는다.
func TestRuntimeIrisBaseURLResolverIgnoresRuntimeEnvChanges(t *testing.T) {
	baseURLFilePath := writeRuntimeIrisBaseURLFile(t, testIrisBaseURLWithSlash)

	resolver, err := newRuntimeIrisBaseURLResolver(settings.IrisConfig{BaseURLFile: baseURLFilePath, H3ServerName: testIrisHost})
	require.NoError(t, err)

	t.Setenv(irisH3ServerNameEnv, "attacker.example")
	t.Setenv(irisBaseURLAllowedHostsEnv, "attacker.example")
	require.NoError(t, os.WriteFile(baseURLFilePath, []byte("https://attacker.example:3001/"), 0o600))

	_, err = resolver.resolve()
	require.ErrorContains(t, err, "host")
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

func TestRuntimeIrisClientSendMessageAcceptedReturnsRequestID(t *testing.T) {
	var (
		gotPath    string
		gotRequest struct {
			Type string `json:"type"`
			Room string `json:"room"`
			Data string `json:"data"`
		}
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("X-Iris-Signature") == "" {
			t.Error("missing iris signature")
		}

		if err := jsonv2.UnmarshalRead(r.Body, &gotRequest); err != nil {
			t.Errorf("decode request: %v", err)
		}

		w.WriteHeader(http.StatusAccepted)

		if err := jsonv2.MarshalWrite(w, iris.ReplyAcceptedResponse{
			Success:   true,
			Delivery:  "queued",
			RequestID: "reply-123",
			Room:      testRoomID,
			Type:      "text",
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := newTestRuntimeIrisClient(t, settings.IrisConfig{BaseURL: server.URL, Transport: iris.TransportHTTP1}, iris.WithHTTPClient(server.Client()))

	resp, err := client.SendMessageAccepted(t.Context(), testRoomID, "hello")
	require.NoError(t, err)
	require.Equal(t, iris.PathReply, gotPath)
	require.Equal(t, "text", gotRequest.Type)
	require.Equal(t, testRoomID, gotRequest.Room)
	require.Equal(t, "hello", gotRequest.Data)
	require.NotNil(t, resp)
	require.Equal(t, "reply-123", resp.RequestID)
	require.Equal(t, "queued", resp.Delivery)
}

func testBaseURLHost(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse test base URL: %v", err)
	}

	return parsed.Hostname()
}
