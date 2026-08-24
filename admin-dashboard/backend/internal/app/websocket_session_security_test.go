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

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))

	_, _, err = conn.ReadMessage()
	require.Error(t, err, "revoked session family must terminate an already-upgraded WebSocket")

	if closeErr, ok := errors.AsType[*websocket.CloseError](err); ok {
		require.Equal(t, websocket.ClosePolicyViolation, closeErr.Code)
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
	_ sessionStore         = (*revocableSessions)(nil)
	_ sessionFamilyChecker = (*revocableSessions)(nil)
	_                      = session.Session{}
)
