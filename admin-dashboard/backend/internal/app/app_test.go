package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
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
		static:          static.NewHandler(),
		wsStreams:       make(chan struct{}, maxSystemStatsStreams),
		openapiJSON:     openapiJSON,
	}
}

func signedSessionCookie(id string) *http.Cookie {
	return &http.Cookie{Name: auth.SessionCookieName, Value: auth.SignSessionID(id, testSecret)}
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
	rec := doRequest(rt.Handler(), httptest.NewRequest(http.MethodGet, "/health", nil))

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

	rec := doRequest(handler, httptest.NewRequest(http.MethodGet, "/admin/api/auth/session", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "tampered.signature"})
	rec = doRequest(handler, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("Set-Cookie"), auth.SessionCookieName+"=;")

	req = httptest.NewRequest(http.MethodGet, "/admin/api/auth/session", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/admin/api/auth/session", nil)
	req.AddCookie(signedSessionCookie("any"))
	rec := doRequest(rt.Handler(), req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestSessionStatusAuthenticated(t *testing.T) {
	sess := liveSession("active-session")
	rt := newTestRuntime(t, storeWith(sess), nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/auth/session", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/admin/api/auth/session", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusUnauthorized, doRequest(handler, req).Code)

	req = httptest.NewRequest(http.MethodPost, "/admin/api/auth/heartbeat", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(handler, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, decodeBody(t, rec), "csrf_token")
}

func TestLoginValidation(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	handler := rt.Handler()

	rec := doRequest(handler, httptest.NewRequest(http.MethodPost, "/admin/api/auth/login", strings.NewReader("not-json")))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	body := strings.NewReader(`{"username":"admin","password":"wrong"}`)
	rec = doRequest(handler, httptest.NewRequest(http.MethodPost, "/admin/api/auth/login", body))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLoginSuccessSetsCookies(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{
		createFn: func(context.Context) (session.Session, error) {
			return *liveSession("fresh-session"), nil
		},
	}, nil)

	body := strings.NewReader(`{"username":"admin","password":"correct-password"}`)
	rec := doRequest(rt.Handler(), httptest.NewRequest(http.MethodPost, "/admin/api/auth/login", body))

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

	req := httptest.NewRequest(http.MethodPost, "/admin/api/auth/login", strings.NewReader(`{"username":"admin","password":"correct-password"}`))
	req.RemoteAddr = "192.0.2.1:50000"
	rec := doRequest(rt.Handler(), req)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Contains(t, decodeBody(t, rec), "retry_after")
}

func TestCSRFEnforcement(t *testing.T) {
	sess := liveSession("csrf-session")
	rt := newTestRuntime(t, storeWith(sess), nil)
	handler := rt.Handler()

	req := httptest.NewRequest(http.MethodPost, "/admin/api/auth/logout", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusForbidden, doRequest(handler, req).Code)

	csrf, err := auth.NewCSRFToken(sess.ID, testSecret)
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodPost, "/admin/api/auth/logout", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: csrf})
	req.Header.Set("X-CSRF-Token", csrf)
	require.Equal(t, http.StatusOK, doRequest(handler, req).Code)
}

func TestCSRFMonitorModeAllows(t *testing.T) {
	sess := liveSession("monitor-session")
	rt := newTestRuntime(t, storeWith(sess), func(cfg *config.Config) {
		cfg.Security.CSRFMode = config.SecurityMonitor
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api/auth/logout", nil)
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

			req := httptest.NewRequest(http.MethodPost, "/admin/api/auth/heartbeat", strings.NewReader(`{"idle":false}`))
			req.AddCookie(signedSessionCookie(sess.ID))
			rec := doRequest(rt.Handler(), req)

			require.Equal(t, tc.wantStatus, rec.Code)
			require.Contains(t, decodeBody(t, rec), tc.wantField)
		})
	}
}

func TestHeartbeatInvalidPayload(t *testing.T) {
	sess := liveSession("hb-bad-payload")
	rt := newTestRuntime(t, storeWith(sess), func(cfg *config.Config) {
		cfg.Security.CSRFMode = config.SecurityOff
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api/auth/heartbeat", strings.NewReader("{broken"))
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusBadRequest, doRequest(rt.Handler(), req).Code)
}

func TestDockerHandlersWithoutDocker(t *testing.T) {
	sess := liveSession("docker-session")
	rt := newTestRuntime(t, storeWith(sess), func(cfg *config.Config) {
		cfg.Security.CSRFMode = config.SecurityOff
	})
	handler := rt.Handler()

	req := httptest.NewRequest(http.MethodGet, "/admin/api/docker/health", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(handler, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, false, decodeBody(t, rec)["available"])

	req = httptest.NewRequest(http.MethodGet, "/admin/api/docker/containers", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusServiceUnavailable, doRequest(handler, req).Code)

	req = httptest.NewRequest(http.MethodPost, "/admin/api/docker/containers/bot/restart", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusServiceUnavailable, doRequest(handler, req).Code)
}

func TestETagRoundTrip(t *testing.T) {
	sess := liveSession("etag-session")
	rt := newTestRuntime(t, storeWith(sess), nil)
	handler := rt.Handler()

	req := httptest.NewRequest(http.MethodGet, "/admin/api/auth/session", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(handler, req)
	require.Equal(t, http.StatusOK, rec.Code)
	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)

	req = httptest.NewRequest(http.MethodGet, "/admin/api/auth/session", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	req.Header.Set("If-None-Match", etag)
	rec = doRequest(handler, req)
	require.Equal(t, http.StatusNotModified, rec.Code)
	require.Empty(t, rec.Body.String())
}

func TestSPAFallbackServesIndexWith200(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	rt.static = static.NewHandlerFromFS(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html lang=\"ko\"></html>")},
	})
	for _, path := range []string{"/", "/dashboard/stats", "/login"} {
		rec := doRequest(rt.Handler(), httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code, "path %s", path)
		require.Contains(t, rec.Header().Get("Content-Type"), "text/html", "path %s", path)
		require.Contains(t, rec.Body.String(), "<!doctype html>", "path %s", path)
	}
}

func TestUnknownAdminAPIRouteIs404JSON(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	rec := doRequest(rt.Handler(), httptest.NewRequest(http.MethodGet, "/admin/api/does-not-exist", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "Not found", decodeBody(t, rec)["error"])
}

func TestMethodNotAllowedIsJSON(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	rec := doRequest(rt.Handler(), httptest.NewRequest(http.MethodPut, "/admin/api/auth/login", nil))

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, "Method not allowed", decodeBody(t, rec)["error"])
}

func TestWSOriginRejected(t *testing.T) {
	sess := liveSession("ws-session")
	rt := newTestRuntime(t, storeWith(sess), nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/ws/system-stats", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	req.Header.Set("Origin", "https://evil.test")
	require.Equal(t, http.StatusForbidden, doRequest(rt.Handler(), req).Code)
}

func TestOpenAPIToggle(t *testing.T) {
	sess := liveSession("openapi-session")
	rt := newTestRuntime(t, storeWith(sess), func(cfg *config.Config) {
		cfg.EnableOpenAPI = false
		cfg.EnableSwaggerUI = false
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/openapi.json", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	require.Equal(t, http.StatusNotFound, doRequest(rt.Handler(), req).Code)

	rt = newTestRuntime(t, storeWith(sess), nil)
	req = httptest.NewRequest(http.MethodGet, "/admin/api/openapi.json", nil)
	req.AddCookie(signedSessionCookie(sess.ID))
	rec := doRequest(rt.Handler(), req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, decodeBody(t, rec), "paths")
}

func TestClientIPRespectsTrustedForwarders(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, func(cfg *config.Config) {
		cfg.TrustedForwarders = true
	})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "10.0.0.9:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	require.Equal(t, "203.0.113.7", rt.clientIP(req))

	rt = newTestRuntime(t, &fakeSessions{}, nil)
	require.Equal(t, "10.0.0.9", rt.clientIP(req))
}
