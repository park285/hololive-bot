package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/observations"
	"github.com/kapu/admin-dashboard/internal/session"
)

type auditRotateAfterRead struct {
	*session.Store

	armed  bool
	winner session.Session
}

func TestLogoutAfterRepeatedRotationRevokesHTTPAndExistingWebSocket(t *testing.T) {
	store := auditStore(t)
	original, err := store.Create(t.Context())
	require.NoError(t, err)

	rt := newTestRuntime(t, store, nil)
	server := httptest.NewServer(rt.Handler())

	defer server.Close()

	headers := http.Header{}

	headers.Set("Sec-WebSocket-Protocol", observations.StatsProtocol())
	headers.Set("Origin", "https://ok.test")
	headers.Set("Cookie", signedSessionCookie(original.ID).String())

	conn, response, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/admin/api/ws/system-stats", headers)
	require.NoError(t, err)

	if response != nil {
		require.NoError(t, response.Body.Close())
	}

	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	first, ok, err := store.Rotate(t.Context(), original.ID)
	require.NoError(t, err)
	require.True(t, ok)

	second, ok, err := store.Rotate(t.Context(), first.ID)
	require.NoError(t, err)
	require.True(t, ok)

	csrf, err := auth.NewCSRFToken(second.ID, testSecret)
	require.NoError(t, err)

	rec := doRequest(rt.Handler(), auditRequest(t, http.MethodPost, "/admin/api/auth/logout", second.ID, csrf, csrf))
	require.Equal(t, http.StatusOK, rec.Code)

	for _, id := range []string{original.ID, first.ID, second.ID} {
		status := doRequest(rt.Handler(), auditRequest(t, http.MethodGet, "/admin/api/auth/session", id, "", ""))
		require.Equal(t, http.StatusUnauthorized, status.Code, "a residual marker must not authenticate")
	}

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))

	for {
		_, _, err = conn.ReadMessage()
		if err != nil {
			break
		}
	}

	closeErr, ok := errors.AsType[*websocket.CloseError](err)
	require.True(t, ok, "revocation must close the connection, not just reach a test timeout: %v", err)
	require.Equal(t, websocket.ClosePolicyViolation, closeErr.Code)
}

func (s *auditRotateAfterRead) Get(ctx context.Context, id string) (session.Session, bool, error) {
	snapshot, found, err := s.Store.Get(ctx, id)
	if err == nil && found && s.armed {
		s.armed = false
		s.winner, _, err = s.Rotate(ctx, id)
	}

	if err != nil {
		return snapshot, found, fmt.Errorf("get session with test rotation: %w", err)
	}

	return snapshot, found, nil
}

func auditStore(t *testing.T) *session.Store {
	t.Helper()

	mr := miniredis.RunT(t)
	cfg := config.DefaultSessionConfig()

	cfg.RotationInterval = 0

	store, err := session.NewStoreWithOptions(t.Context(), mr.Addr(), &cfg, session.Options{DisableCache: true, ForceSingleClient: true})
	require.NoError(t, err)
	t.Cleanup(store.Close)

	return store
}

func auditRequest(t *testing.T, method, path, id, cookieCSRF, headerCSRF string) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, http.NoBody)
	req.AddCookie(signedSessionCookie(id))
	req.Header.Set("Cookie", req.Header.Get("Cookie")+"; "+auth.CSRFCookieName+"="+cookieCSRF)
	req.Header.Set("X-CSRF-Token", headerCSRF)

	return req
}

func TestLogoutRevokesRotationAfterAuthenticationRead(t *testing.T) {
	backingStore := auditStore(t)
	original, err := backingStore.Create(t.Context())
	require.NoError(t, err)

	csrf, err := auth.NewCSRFToken(original.ID, testSecret)
	require.NoError(t, err)

	store := &auditRotateAfterRead{Store: backingStore, armed: true}
	rt := newTestRuntime(t, store, nil)
	rec := doRequest(rt.Handler(), auditRequest(t, http.MethodPost, "/admin/api/auth/logout", original.ID, csrf, csrf))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, clearsAuthCookies(rec))
	require.NotEmpty(t, store.winner.ID)

	_, alive, err := backingStore.Get(t.Context(), store.winner.ID)
	require.NoError(t, err)

	active, err := backingStore.FamilyActive(t.Context(), original.FamilyID)
	require.NoError(t, err)

	status := doRequest(rt.Handler(), auditRequest(t, http.MethodGet, "/admin/api/auth/session", store.winner.ID, "", ""))
	t.Logf("logout=%d replacement_alive=%t family_active=%t replacement_auth=%d", rec.Code, alive, active, status.Code)
	require.False(t, alive, "replacement must not survive successful logout")
	require.False(t, active)
	require.Equal(t, http.StatusUnauthorized, status.Code)

	rotated, ok, err := backingStore.Rotate(t.Context(), store.winner.ID)
	require.NoError(t, err)
	require.False(t, ok, "late rotation must not resurrect the family")
	require.Empty(t, rotated.ID)
}
