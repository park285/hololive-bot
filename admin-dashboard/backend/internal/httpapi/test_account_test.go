package httpapi

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/observations"
	"github.com/kapu/admin-dashboard/internal/session"
)

type countingTestAccountStore struct {
	*session.Store

	claims atomic.Int32
}

func (s *countingTestAccountStore) ClaimMutation(ctx context.Context, sess session.Session, id string) (bool, error) {
	s.claims.Add(1)

	claimed, err := s.Store.ClaimMutation(ctx, sess, id)
	if err != nil {
		return claimed, fmt.Errorf("claim test mutation: %w", err)
	}

	return claimed, nil
}

func newTestAccountRuntime(t *testing.T) (*API, *countingTestAccountStore, session.TestCredentials) {
	t.Helper()

	cfg := config.DefaultSessionConfig()

	cfg.RotationInterval = 0

	store, err := session.NewStoreWithOptions(t.Context(), miniredis.RunT(t).Addr(), &cfg, session.Options{DisableCache: true, ForceSingleClient: true})
	require.NoError(t, err)
	t.Cleanup(store.Close)

	account, credentials, err := session.NewTestAccount(time.Minute, 10)
	require.NoError(t, err)
	require.NoError(t, store.IssueTestAccount(t.Context(), account))

	wrapped := &countingTestAccountStore{Store: store}

	return newTestRuntime(t, wrapped, nil), wrapped, credentials
}

func accountRequest(t *testing.T, client *http.Client, base, method, path, csrf string, body any) (int, map[string]any) {
	t.Helper()

	data, err := jsonv2.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), method, base+path, bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Client-Generation", contract.Generation)

	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}

	response, err := client.Do(req)
	if err != nil || response == nil {
		t.Fatalf("응답을 받지 못했습니다: %v", err)

		return 0, nil
	}

	defer response.Body.Close()

	var payload map[string]any

	require.NoError(t, jsonv2.UnmarshalRead(response.Body, &payload))

	return response.StatusCode, payload
}

func newAccountClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	return &http.Client{Jar: jar, Timeout: 5 * time.Second}
}

func TestTestAccountUsesLoginCSRFAndRejectsEveryBusinessMutation(t *testing.T) {
	rt, store, credentials := newTestAccountRuntime(t)
	server := httptest.NewServer(rt.Handler())

	defer server.Close()

	client := newAccountClient(t)
	status, body := accountRequest(t, client, server.URL, http.MethodPost, "/admin/api/auth/login", "", loginRequest{Username: credentials.Username, Password: credentials.Password})
	require.Equal(t, http.StatusOK, status)

	csrf, ok := body["csrf_token"].(string)
	require.True(t, ok)

	status, body = accountRequest(t, client, server.URL, http.MethodGet, "/admin/api/auth/session", "", nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, credentials.Username, body["username"])

	status, _ = accountRequest(t, client, server.URL, http.MethodGet, "/admin/api/status", "", nil)
	require.Equal(t, http.StatusOK, status)

	status, _ = accountRequest(t, client, server.URL, http.MethodPost, heartbeatPath, "", map[string]bool{"idle": false})
	require.Equal(t, http.StatusForbidden, status)

	status, body = accountRequest(t, client, server.URL, http.MethodPost, heartbeatPath, csrf, map[string]bool{"idle": false})
	require.Equal(t, http.StatusOK, status)

	csrf, ok = body["csrf_token"].(string)
	require.True(t, ok)

	for _, endpoint := range rt.endpoints() {
		if endpoint.access != businessMutation {
			continue
		}

		path := strings.NewReplacer("{id}", "9007199254740993", "{name}", "hololive-api").Replace(endpoint.path)
		mutationStatus, _ := accountRequest(t, client, server.URL, endpoint.method, path, csrf, map[string]string{})
		require.Equal(t, http.StatusForbidden, mutationStatus, "%s %s", endpoint.method, path)
	}

	require.Zero(t, store.claims.Load(), "조회 전용 계정은 mutation claim 전에 거부합니다")

	status, _ = accountRequest(t, client, server.URL, http.MethodPost, "/admin/api/auth/logout", csrf, map[string]string{})
	require.Equal(t, http.StatusOK, status)

	status, _ = accountRequest(t, client, server.URL, http.MethodGet, "/admin/api/auth/session", "", nil)
	require.Equal(t, http.StatusUnauthorized, status)

	primary := newAccountClient(t)

	status, _ = accountRequest(t, primary, server.URL, http.MethodPost, "/admin/api/auth/login", "", loginRequest{Username: "admin", Password: "correct-password"})
	require.Equal(t, http.StatusOK, status, "기존 관리자 비밀번호가 유지됩니다")
}

func TestTestAccountRevocationClosesAuthenticatedWebSocketAndHTTP(t *testing.T) {
	rt, store, credentials := newTestAccountRuntime(t)
	server := httptest.NewServer(rt.Handler())

	defer server.Close()

	client := newAccountClient(t)
	status, _ := accountRequest(t, client, server.URL, http.MethodPost, "/admin/api/auth/login", "", loginRequest{Username: credentials.Username, Password: credentials.Password})
	require.Equal(t, http.StatusOK, status)

	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)

	cookieRequest := &http.Request{Header: make(http.Header)}

	for _, cookie := range client.Jar.Cookies(parsed) {
		cookieRequest.AddCookie(cookie)
	}

	headers := cookieRequest.Header
	headers.Set("Origin", "https://ok.test")
	headers.Set("Sec-WebSocket-Protocol", observations.StatsProtocol())

	conn, handshake, err := websocket.DefaultDialer.DialContext(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http")+"/admin/api/ws/system-stats", headers)
	if handshake != nil {
		defer handshake.Body.Close()
	}

	require.NoError(t, err)

	defer func() { require.NoError(t, conn.Close()) }()

	require.Equal(t, http.StatusSwitchingProtocols, handshake.StatusCode)

	changed, err := store.RevokeTestAccount(t.Context(), credentials.Username)
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))

	_, _, err = conn.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation), "폐기된 계정의 WS가 종료되어야 합니다")

	status, _ = accountRequest(t, client, server.URL, http.MethodGet, "/admin/api/auth/session", "", nil)
	require.Equal(t, http.StatusUnauthorized, status)

	status, _ = accountRequest(t, client, server.URL, http.MethodPost, "/admin/api/auth/login", "", loginRequest{Username: credentials.Username, Password: credentials.Password})
	require.Equal(t, http.StatusUnauthorized, status)
}

func TestTestAccountSuccessCannotResetAdministratorFailureBudget(t *testing.T) {
	rt, _, credentials := newTestAccountRuntime(t)

	for range 30 {
		_, err := rt.distributedLoginLimiter.RecordFailure(t.Context(), "192.0.2.1", rt.cfg.AdminUser)
		require.NoError(t, err)
	}

	server := httptest.NewServer(rt.Handler())

	defer server.Close()

	status, _ := accountRequest(t, newAccountClient(t), server.URL, http.MethodPost, "/admin/api/auth/login", "", loginRequest{Username: credentials.Username, Password: credentials.Password})
	require.Equal(t, http.StatusOK, status)

	retry, err := rt.distributedLoginLimiter.Check(t.Context(), "192.0.2.2", rt.cfg.AdminUser)
	require.NoError(t, err)
	require.Positive(t, retry)
}
