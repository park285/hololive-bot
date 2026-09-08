package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/session"
)

type revocableSessions struct {
	*fakeSessions

	active atomic.Bool
}

func (s *revocableSessions) FamilyActive(context.Context, string) (bool, error) {
	return s.active.Load(), nil
}

func TestSystemStatsWSClosesWhenSessionFamilyIsRevoked(t *testing.T) {
	sess := liveSession("ws-revocation-session")

	sess.FamilyID = "ws-revocation-family"

	store := &revocableSessions{fakeSessions: storeWith(sess)}
	store.active.Store(true)

	rt := newTestRuntime(t, store, nil)

	server := httptest.NewServer(rt.Handler())
	defer server.Close()

	header := http.Header{}
	header.Set("Origin", "https://ok.test")
	header.Set("Cookie", signedSessionCookie(sess.ID).String())

	conn, resp, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/admin/api/ws/system-stats", header)
	require.NoError(t, err)

	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}

	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	store.active.Store(false)
	requireRevocationClose(t, conn, 3*time.Second)
}

func TestSystemStatsWSClosesWhenSessionAbsolutelyExpires(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := config.DefaultSessionConfig()

	cfg.ExpiryDuration = 2 * time.Second
	cfg.AbsoluteTimeout = 1500 * time.Millisecond

	store, err := session.NewStoreWithOptions(t.Context(), mr.Addr(), &cfg, session.Options{DisableCache: true, ForceSingleClient: true})
	require.NoError(t, err)
	t.Cleanup(store.Close)

	sess, err := store.Create(t.Context())
	require.NoError(t, err)

	rt := newTestRuntime(t, store, nil)
	server := httptest.NewServer(rt.Handler())

	defer server.Close()

	conn := dialSystemStatsWebSocket(t, server.URL, sess.ID)
	requireRevocationClose(t, conn, 4*time.Second)
}

func TestSystemStatsWSClosesWhenSessionStoreFails(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := config.DefaultSessionConfig()

	store, err := session.NewStoreWithOptions(t.Context(), mr.Addr(), &cfg, session.Options{DisableCache: true, ForceSingleClient: true})
	require.NoError(t, err)
	t.Cleanup(store.Close)

	sess, err := store.Create(t.Context())
	require.NoError(t, err)

	rt := newTestRuntime(t, store, nil)
	server := httptest.NewServer(rt.Handler())

	defer server.Close()

	conn := dialSystemStatsWebSocket(t, server.URL, sess.ID)

	mr.Close()
	requireRevocationClose(t, conn, 3*time.Second)
}

func dialSystemStatsWebSocket(t *testing.T, serverURL, sessionID string) *websocket.Conn {
	t.Helper()

	header := http.Header{}
	header.Set("Origin", "https://ok.test")
	header.Set("Cookie", signedSessionCookie(sessionID).String())

	conn, resp, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(serverURL, "http")+"/admin/api/ws/system-stats", header)
	require.NoError(t, err)

	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}

	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	return conn
}

func requireRevocationClose(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(timeout)))

	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue
		}

		closeErr, ok := errors.AsType[*websocket.CloseError](err)
		require.True(t, ok, "session invalidation must close the connection, not just reach a test timeout: %v", err)
		require.Equal(t, websocket.ClosePolicyViolation, closeErr.Code)

		return
	}
}

func TestSystemStatsWSLimitSurvivesTokenRotation(t *testing.T) {
	old := liveSession("ws-family-old")

	old.FamilyID = "stable-ws-family"

	newSession := liveSession("ws-family-new")

	newSession.FamilyID = old.FamilyID

	store := storeWithSessions(old, newSession)
	rt := newTestRuntime(t, store, nil)

	server := httptest.NewServer(rt.Handler())
	defer server.Close()

	dialURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/admin/api/ws/system-stats"

	conns := make([]*websocket.Conn, 0, maxStreamsPerSession)

	t.Cleanup(func() {
		for _, conn := range conns {
			require.NoError(t, conn.Close())
		}
	})

	ids := []string{old.ID, newSession.ID, old.ID, newSession.ID}
	for _, id := range ids {
		header := http.Header{}
		header.Set("Origin", "https://ok.test")
		header.Set("Cookie", signedSessionCookie(id).String())

		conn, resp, err := websocket.DefaultDialer.Dial(dialURL, header)
		require.NoError(t, err)

		if resp != nil {
			require.NoError(t, resp.Body.Close())
		}

		conns = append(conns, conn)
	}

	header := http.Header{}
	header.Set("Origin", "https://ok.test")
	header.Set("Cookie", signedSessionCookie(newSession.ID).String())

	_, resp, err := websocket.DefaultDialer.Dial(dialURL, header)
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}

var (
	_ sessionStore = (*revocableSessions)(nil)
	_              = session.Session{}
)
