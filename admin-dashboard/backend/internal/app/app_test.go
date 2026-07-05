package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/park285/shared-go/pkg/json"
	"github.com/park285/shared-go/pkg/logging"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/openapi"
	"github.com/kapu/admin-dashboard/internal/session"
	"github.com/kapu/admin-dashboard/internal/static"
	"github.com/kapu/admin-dashboard/internal/status"
)

const testSecret = "0123456789abcdef-secret"

type fakeSessions struct {
	createFn  func(ctx context.Context) (session.Session, error)
	getFn     func(ctx context.Context, id string) (*session.Session, error)
	deleteFn  func(ctx context.Context, id string) error
	refreshFn func(ctx context.Context, id string, idle bool) (session.RefreshResult, error)
	rotateFn  func(ctx context.Context, oldID string) (*session.Session, error)
}

func (f *fakeSessions) Create(ctx context.Context) (session.Session, error) {
	if f.createFn == nil {
		return session.Session{ID: "created-session"}, nil
	}
	return f.createFn(ctx)
}

func (f *fakeSessions) Get(ctx context.Context, id string) (*session.Session, error) {
	if f.getFn == nil {
		return nil, nil
	}
	return f.getFn(ctx, id)
}

func (f *fakeSessions) Delete(ctx context.Context, id string) error {
	if f.deleteFn == nil {
		return nil
	}
	return f.deleteFn(ctx, id)
}

func (f *fakeSessions) Refresh(ctx context.Context, id string, idle bool) (session.RefreshResult, error) {
	if f.refreshFn == nil {
		return session.RefreshResult{Kind: session.RefreshMissing}, nil
	}
	return f.refreshFn(ctx, id, idle)
}

func (f *fakeSessions) Rotate(ctx context.Context, oldID string) (*session.Session, error) {
	if f.rotateFn == nil {
		return nil, nil
	}
	return f.rotateFn(ctx, oldID)
}

func (f *fakeSessions) Close() {}

func liveSession(id string) *session.Session {
	now := time.Now().UTC()
	return &session.Session{
		ID:                id,
		CreatedAt:         now,
		ExpiresAt:         now.Add(30 * time.Minute),
		AbsoluteExpiresAt: now.Add(8 * time.Hour),
		LastRotatedAt:     now,
	}
}

func storeWith(sess *session.Session) *fakeSessions {
	return &fakeSessions{
		getFn: func(_ context.Context, id string) (*session.Session, error) {
			if sess != nil && sess.ID == id {
				return sess, nil
			}
			return nil, nil
		},
	}
}

func testPasswordHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return string(hash)
}

func newTestRuntime(t *testing.T, store sessionStore, mutate func(*config.Config)) *Runtime {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := config.Config{
		Env:           "test",
		AdminUser:     "admin",
		AdminPassHash: testPasswordHash(t, "correct-password"),
		SessionSecret: testSecret,
		Security: config.SecurityConfig{
			AllowedOrigins: []string{"https://ok.test"},
			CSRFMode:       config.SecurityEnforce,
			WSOriginMode:   config.SecurityEnforce,
		},
		Session:         config.DefaultSessionConfig(),
		EnableOpenAPI:   true,
		EnableSwaggerUI: true,
		RuntimeVersion:  "test",
	}
	if mutate != nil {
		mutate(&cfg)
	}
	openapiJSON, err := json.Marshal(openapi.Spec(cfg.RuntimeVersion))
	require.NoError(t, err)
	return &Runtime{
		cfg:             cfg,
		logger:          logging.NewTestLogger(),
		sessions:        store,
		rateLimiter:     auth.NewLoginRateLimiter(),
		statusCollector: status.NewCollector(nil, "test"),
		statsHub:        status.NewHub(nil),
		static:          static.NewHandler(),
		wsStreams:       make(chan struct{}, maxSystemStatsStreams),
		wsPongWait:      defaultWSPongWait,
		wsPingPeriod:    defaultWSPingPeriod,
		openapiJSON:     openapiJSON,
	}
}

func signedSessionCookie(id string) *http.Cookie {
	return &http.Cookie{Name: auth.SessionCookieName, Value: auth.SignSessionID(id, testSecret), Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

func doRequest(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload), "body: %s", rec.Body.String())
	return payload
}

func TestHealthAndSecurityHeaders(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, func(cfg *config.Config) {
		cfg.Security.ForceHTTPS = true
	})
	rec := doRequest(rt.Handler(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", http.NoBody))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", decodeBody(t, rec)["status"])
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	require.NotEmpty(t, rec.Header().Get("Content-Security-Policy"))
	require.NotEmpty(t, rec.Header().Get("Strict-Transport-Security"))
}

func TestAuthMiddlewareRejections(t *testing.T) {
	rt := newTestRuntime(t, storeWith(nil), nil)
	handler := rt.Handler()

	rec := doRequest(handler, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/auth/session", http.NoBody))
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/auth/session", http.NoBody)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tampered.signature", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rec = doRequest(handler, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("Set-Cookie"), auth.SessionCookieName+"=;")

	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/auth/session", http.NoBody)
	req.AddCookie(signedSessionCookie("missing-session"))
	rec = doRequest(handler, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddlewareStoreError(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{
		getFn: func(context.Context, string) (*session.Session, error) {
			return nil, errors.New("valkey down")
		},
	}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/auth/session", http.NoBody)
	req.AddCookie(signedSessionCookie("any"))
	rec := doRequest(rt.Handler(), req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestSessionStatusAuthenticated(t *testing.T) {
	sess := liveSession("active-session")
	rt := newTestRuntime(t, storeWith(sess), nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/auth/session", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(rt.Handler(), req)

	require.Equal(t, http.StatusOK, rec.Code)
	payload := decodeBody(t, rec)
	require.Equal(t, true, payload["authenticated"])
	require.Equal(t, "admin", payload["username"])
	require.Contains(t, payload, "session_policy")
}

func TestRotatedSessionOnlyAllowsHeartbeat(t *testing.T) {
	rotatedTo := "next-session"
	sess := liveSession("rotated-session")
	sess.RotatedTo = &rotatedTo
	store := storeWith(sess)
	store.refreshFn = func(context.Context, string, bool) (session.RefreshResult, error) {
		return session.RefreshResult{Kind: session.RefreshRotated, Session: liveSession(rotatedTo)}, nil
	}
	rt := newTestRuntime(t, store, func(cfg *config.Config) {
		cfg.Security.CSRFMode = config.SecurityOff
	})
	handler := rt.Handler()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/auth/session", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusUnauthorized, doRequest(handler, req).Code)

	req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/heartbeat", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(handler, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, decodeBody(t, rec), "csrf_token")
}

func TestLoginValidation(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	handler := rt.Handler()

	rec := doRequest(handler, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/login", strings.NewReader("not-json")))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	body := strings.NewReader(`{"username":"admin","password":"wrong"}`)
	rec = doRequest(handler, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/login", body))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLoginSuccessSetsCookies(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{
		createFn: func(context.Context) (session.Session, error) {
			return *liveSession("fresh-session"), nil
		},
	}, nil)

	body := strings.NewReader(`{"username":"admin","password":"correct-password"}`)
	rec := doRequest(rt.Handler(), httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/login", body))

	require.Equal(t, http.StatusOK, rec.Code)
	payload := decodeBody(t, rec)
	require.Equal(t, "Login successful", payload["message"])
	require.NotEmpty(t, payload["csrf_token"])
	cookies := rec.Result().Cookies()
	names := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		names = append(names, cookie.Name)
	}
	require.Contains(t, names, auth.SessionCookieName)
	require.Contains(t, names, auth.CSRFCookieName)
}

func TestLoginRateLimited(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	for range 5 {
		rt.rateLimiter.RecordFailure("192.0.2.1")
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/login", strings.NewReader(`{"username":"admin","password":"correct-password"}`))
	req.RemoteAddr = "192.0.2.1:50000"
	rec := doRequest(rt.Handler(), req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Contains(t, decodeBody(t, rec), "retry_after")
}

func TestCSRFEnforcement(t *testing.T) {
	sess := liveSession("csrf-session")
	rt := newTestRuntime(t, storeWith(sess), nil)
	handler := rt.Handler()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/logout", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusForbidden, doRequest(handler, req).Code)

	csrf, err := auth.NewCSRFToken(sess.ID, testSecret)
	require.NoError(t, err)
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/logout", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: csrf, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	req.Header.Set("X-CSRF-Token", csrf)
	require.Equal(t, http.StatusOK, doRequest(handler, req).Code)
}

func TestCSRFMonitorModeAllows(t *testing.T) {
	sess := liveSession("monitor-session")
	rt := newTestRuntime(t, storeWith(sess), func(cfg *config.Config) {
		cfg.Security.CSRFMode = config.SecurityMonitor
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/logout", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusOK, doRequest(rt.Handler(), req).Code)
}

func TestHeartbeatResultContract(t *testing.T) {
	sess := liveSession("hb-session")
	cases := []struct {
		name       string
		result     session.RefreshResult
		rotated    *session.Session
		wantStatus int
		wantField  string
	}{
		{"refreshed", session.RefreshResult{Kind: session.RefreshRefreshed, Session: sess}, nil, http.StatusOK, "absolute_expires_at"},
		{"refreshed_with_rotation", session.RefreshResult{Kind: session.RefreshRefreshed, Session: sess}, liveSession("rotated-next"), http.StatusOK, "csrf_token"},
		{"rotated", session.RefreshResult{Kind: session.RefreshRotated, Session: sess}, nil, http.StatusOK, "csrf_token"},
		{"idle", session.RefreshResult{Kind: session.RefreshIdleShortened}, nil, http.StatusOK, "idle_rejected"},
		{"absolute_expired", session.RefreshResult{Kind: session.RefreshAbsoluteExpired}, nil, http.StatusUnauthorized, "absolute_expired"},
		{"missing", session.RefreshResult{Kind: session.RefreshMissing}, nil, http.StatusUnauthorized, "error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := storeWith(sess)
			store.refreshFn = func(context.Context, string, bool) (session.RefreshResult, error) {
				return tc.result, nil
			}
			store.rotateFn = func(context.Context, string) (*session.Session, error) {
				return tc.rotated, nil
			}
			rt := newTestRuntime(t, store, func(cfg *config.Config) {
				cfg.Security.CSRFMode = config.SecurityOff
			})

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/heartbeat", strings.NewReader(`{"idle":false}`))
			req.AddCookie(signedSessionCookie(sess.ID))
			rec := doRequest(rt.Handler(), req)

			require.Equal(t, tc.wantStatus, rec.Code)
			require.Contains(t, decodeBody(t, rec), tc.wantField)
		})
	}
}

func TestPlainHeartbeatReissuesSessionCookie(t *testing.T) {
	cases := []struct {
		name            string
		rotationEnabled bool
	}{
		{"rotation_disabled", false},
		{"rotation_enabled_not_due", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := liveSession("hb-cookie-session")
			store := storeWith(sess)
			store.refreshFn = func(context.Context, string, bool) (session.RefreshResult, error) {
				return session.RefreshResult{Kind: session.RefreshRefreshed, Session: sess}, nil
			}
			store.rotateFn = func(context.Context, string) (*session.Session, error) {
				return nil, nil
			}
			rt := newTestRuntime(t, store, func(cfg *config.Config) {
				cfg.Security.CSRFMode = config.SecurityOff
				cfg.Session.TokenRotationEnabled = tc.rotationEnabled
			})

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/heartbeat", strings.NewReader(`{"idle":false}`))
			req.AddCookie(signedSessionCookie(sess.ID))
			rec := doRequest(rt.Handler(), req)

			require.Equal(t, http.StatusOK, rec.Code)
			payload := decodeBody(t, rec)
			require.Contains(t, payload, "absolute_expires_at")
			require.NotContains(t, payload, "csrf_token")
			var sessionCookie *http.Cookie
			for _, cookie := range rec.Result().Cookies() {
				if cookie.Name == auth.SessionCookieName {
					sessionCookie = cookie
				}
			}
			require.NotNil(t, sessionCookie, "plain heartbeat must re-issue the session cookie")
			require.Positive(t, sessionCookie.MaxAge)
		})
	}
}

func TestHeartbeatInvalidPayload(t *testing.T) {
	sess := liveSession("hb-bad-payload")
	rt := newTestRuntime(t, storeWith(sess), func(cfg *config.Config) {
		cfg.Security.CSRFMode = config.SecurityOff
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/heartbeat", strings.NewReader("{broken"))
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusBadRequest, doRequest(rt.Handler(), req).Code)
}

func TestDockerHandlersWithoutDocker(t *testing.T) {
	sess := liveSession("docker-session")
	rt := newTestRuntime(t, storeWith(sess), func(cfg *config.Config) {
		cfg.Security.CSRFMode = config.SecurityOff
	})
	handler := rt.Handler()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/docker/health", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(handler, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, false, decodeBody(t, rec)["available"])

	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/docker/containers", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusServiceUnavailable, doRequest(handler, req).Code)

	req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/docker/containers/bot/restart", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusServiceUnavailable, doRequest(handler, req).Code)
}

func TestETagRoundTrip(t *testing.T) {
	sess := liveSession("etag-session")
	rt := newTestRuntime(t, storeWith(sess), nil)
	handler := rt.Handler()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/docker/health", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(handler, req)
	require.Equal(t, http.StatusOK, rec.Code)
	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)

	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/docker/health", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	req.Header.Set("If-None-Match", etag)
	rec = doRequest(handler, req)
	require.Equal(t, http.StatusNotModified, rec.Code)
	require.Empty(t, rec.Body.String())
}

func TestETagSkippedForLargeBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	writer := &etagWriter{ResponseWriter: c.Writer, limit: 8}

	_, err := writer.WriteString("hello")
	require.NoError(t, err)
	large := strings.Repeat("x", 100)
	_, err = writer.WriteString(large)
	require.NoError(t, err)
	writer.flush(httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/status", http.NoBody))

	require.True(t, writer.overflow)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("ETag"))
	require.Equal(t, "hello"+large, rec.Body.String())
}

func TestSystemStatsWSPerSessionCap(t *testing.T) {
	sess := liveSession("ws-cap-session")
	rt := newTestRuntime(t, storeWith(sess), nil)

	server := httptest.NewServer(rt.Handler())
	defer server.Close()

	header := http.Header{}
	header.Set("Origin", "https://ok.test")
	header.Set("Cookie", signedSessionCookie(sess.ID).String())
	dialURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/admin/api/ws/system-stats"

	conns := make([]*websocket.Conn, 0, maxStreamsPerSession)
	defer func() {
		for _, conn := range conns {
			if cerr := conn.Close(); cerr != nil {
				continue
			}
		}
	}()
	for range maxStreamsPerSession {
		conn, resp, err := websocket.DefaultDialer.Dial(dialURL, header)
		require.NoError(t, err)
		if resp != nil {
			require.NoError(t, resp.Body.Close())
		}
		conns = append(conns, conn)
	}

	_, resp, err := websocket.DefaultDialer.Dial(dialURL, header)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}

func TestSessionStatusIssuesCSRFTokenForBootstrap(t *testing.T) {
	sess := liveSession("bootstrap-session")
	rt := newTestRuntime(t, storeWith(sess), nil)
	handler := rt.Handler()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/auth/session", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(handler, req)
	require.Equal(t, http.StatusOK, rec.Code)

	token, ok := decodeBody(t, rec)["csrf_token"].(string)
	require.True(t, ok)
	require.NotEmpty(t, token, "/auth/session must issue a CSRF token so the SPA recovers it after a page refresh")

	var csrfCookie *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == auth.CSRFCookieName {
			csrfCookie = ck
		}
	}
	require.NotNil(t, csrfCookie, "/auth/session must re-set the CSRF cookie")
	require.Equal(t, token, csrfCookie.Value, "double-submit cookie and body token must match")

	post := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/api/auth/logout", http.NoBody)
	post.AddCookie(signedSessionCookie(sess.ID))
	post.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: token, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	post.Header.Set("X-CSRF-Token", token)
	require.Equal(t, http.StatusOK, doRequest(handler, post).Code,
		"the token issued by /auth/session must satisfy the CSRF gate on the next mutation")
}

func TestSessionStatusPreservesValidCSRFToken(t *testing.T) {
	sess := liveSession("csrf-preserve-session")
	rt := newTestRuntime(t, storeWith(sess), nil)
	handler := rt.Handler()
	csrf, err := auth.NewCSRFToken(sess.ID, testSecret)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/auth/session", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: csrf, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rec := doRequest(handler, req)
	require.Equal(t, http.StatusOK, rec.Code)

	payloadToken, ok := decodeBody(t, rec)["csrf_token"].(string)
	require.True(t, ok)
	require.Equal(t, csrf, payloadToken)

	var csrfCookie *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == auth.CSRFCookieName {
			csrfCookie = ck
		}
	}
	require.NotNil(t, csrfCookie)
	require.Equal(t, csrf, csrfCookie.Value)
}

func TestSPAFallbackServesIndexWith200(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	rt.static = static.NewHandlerFromFS(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html lang=\"ko\"></html>")},
	})
	for _, path := range []string{"/", "/dashboard/stats", "/login"} {
		rec := doRequest(rt.Handler(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, http.NoBody))
		require.Equal(t, http.StatusOK, rec.Code, "path %s", path)
		require.Contains(t, rec.Header().Get("Content-Type"), "text/html", "path %s", path)
		require.Contains(t, rec.Body.String(), "<!doctype html>", "path %s", path)
	}
}

func TestUnknownAdminAPIRouteIs404JSON(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	rec := doRequest(rt.Handler(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/does-not-exist", http.NoBody))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "Not found", decodeBody(t, rec)["error"])
}

func TestMethodNotAllowedIsJSON(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	rec := doRequest(rt.Handler(), httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/admin/api/auth/login", http.NoBody))

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, "Method not allowed", decodeBody(t, rec)["error"])
}

func TestWSOriginRejected(t *testing.T) {
	sess := liveSession("ws-session")
	rt := newTestRuntime(t, storeWith(sess), nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/ws/system-stats", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	req.Header.Set("Origin", "https://evil.test")
	require.Equal(t, http.StatusForbidden, doRequest(rt.Handler(), req).Code)
}

func TestSystemStatsWSReplaysHistoryOnConnect(t *testing.T) {
	sess := liveSession("ws-replay-session")
	rt := newTestRuntime(t, storeWith(sess), nil)
	rt.statsHub.Publish(&status.SystemStats{ThreadCount: 1})
	rt.statsHub.Publish(&status.SystemStats{ThreadCount: 2})

	server := httptest.NewServer(rt.Handler())
	defer server.Close()

	header := http.Header{}
	header.Set("Origin", "https://ok.test")
	header.Set("Cookie", signedSessionCookie(sess.ID).String())
	conn, resp, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/admin/api/ws/system-stats", header)
	require.NoError(t, err)
	if resp != nil {
		defer func() {
			require.NoError(t, resp.Body.Close())
		}()
	}
	defer func() {
		require.NoError(t, conn.Close())
	}()

	for want := 1; want <= 2; want++ {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
		var frame status.SystemStats
		require.NoError(t, conn.ReadJSON(&frame))
		require.Equal(t, want, frame.ThreadCount)
	}
}

func TestSystemStatsWSReapsSilentPeer(t *testing.T) {
	sess := liveSession("ws-reap-session")
	rt := newTestRuntime(t, storeWith(sess), nil)
	rt.wsPongWait = 200 * time.Millisecond
	rt.wsPingPeriod = 60 * time.Millisecond

	server := httptest.NewServer(rt.Handler())
	defer server.Close()

	header := http.Header{}
	header.Set("Origin", "https://ok.test")
	header.Set("Cookie", signedSessionCookie(sess.ID).String())
	conn, resp, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/admin/api/ws/system-stats", header)
	require.NoError(t, err)
	if resp != nil {
		defer func() { require.NoError(t, resp.Body.Close()) }()
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			return
		}
	}()

	conn.SetPingHandler(func(string) error { return nil })

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, _, readErr := conn.ReadMessage()
	require.Error(t, readErr, "server must reap a peer that stops answering pings instead of leaking its stream slot")
	if netErr, ok := errors.AsType[net.Error](readErr); ok {
		require.False(t, netErr.Timeout(),
			"client read timed out — server never reaped the silent peer (wsStreams slot leak regression)")
	}
}

func TestCloseConnClosesServerSideConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, req, nil)
		if err != nil {
			return
		}
		closeConn(conn)
	}))
	defer srv.Close()

	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			return
		}
	}()
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			return
		}
	}()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, _, readErr := conn.ReadMessage()
	require.Error(t, readErr, "server must close the connection after the handler returns")
	if netErr, ok := errors.AsType[net.Error](readErr); ok {
		require.False(t, netErr.Timeout(),
			"read timed out instead of seeing a server close — closeConn no longer closes the conn (FD leak regression)")
	}
}

func TestOpenAPIToggle(t *testing.T) {
	sess := liveSession("openapi-session")
	rt := newTestRuntime(t, storeWith(sess), func(cfg *config.Config) {
		cfg.EnableOpenAPI = false
		cfg.EnableSwaggerUI = false
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/openapi.json", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusNotFound, doRequest(rt.Handler(), req).Code)

	rt = newTestRuntime(t, storeWith(sess), nil)
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/openapi.json", http.NoBody)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(rt.Handler(), req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, decodeBody(t, rec), "paths")
}

func mustCIDR(t *testing.T) netip.Prefix {
	t.Helper()
	prefix, err := netip.ParsePrefix("10.0.0.0/8")
	require.NoError(t, err)
	return prefix.Masked()
}

func TestHB04ForwardedHeaderIgnoredFromUntrustedPeer_e8fc8b7d(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, func(cfg *config.Config) {
		cfg.TrustedForwarders = true
		cfg.TrustedProxyCIDRs = []netip.Prefix{mustCIDR(t)}
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", http.NoBody)
	req.RemoteAddr = "203.0.113.50:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	require.Equal(t, "203.0.113.50", rt.clientIP(req),
		"untrusted peer must not be able to override clientIP via X-Forwarded-For")
}

func TestHB04ForwardedHeaderRotationDoesNotResetRateLimit_e8fc8b7d(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, func(cfg *config.Config) {
		cfg.TrustedForwarders = true
		cfg.TrustedProxyCIDRs = []netip.Prefix{mustCIDR(t)}
	})
	keys := make(map[string]struct{})
	for i := range 8 {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", http.NoBody)
		req.RemoteAddr = "203.0.113.50:4321"
		req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i))
		keys[rt.clientIP(req)] = struct{}{}
	}
	require.Len(t, keys, 1, "rotated forwarded headers from one untrusted peer must collapse to a single limiter key")
	for key := range keys {
		require.Equal(t, "203.0.113.50", key)
	}
}

func TestHB04TrustedProxyXFFAcceptedFromAllowlistedPeer_e8fc8b7d(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, func(cfg *config.Config) {
		cfg.TrustedForwarders = true
		cfg.TrustedProxyCIDRs = []netip.Prefix{mustCIDR(t)}
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", http.NoBody)
	req.RemoteAddr = "10.0.0.9:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	require.Equal(t, "203.0.113.7", rt.clientIP(req),
		"XFF from an allowlisted proxy chain must resolve to the rightmost non-trusted hop")

	rt = newTestRuntime(t, &fakeSessions{}, nil)
	require.Equal(t, "10.0.0.9", rt.clientIP(req),
		"with forwarding disabled the peer host is the key")
}
