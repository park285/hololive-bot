package observations

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/contract"
)

const streamTestOrigin = "https://admin.test"

func streamTestPolicy() StreamPolicy {
	return StreamPolicy{
		AllowedOrigins: []string{streamTestOrigin}, OriginMode: config.SecurityEnforce,
		Authorize:    func(*http.Request) (string, error) { return "family", nil },
		FamilyActive: func(context.Context, string) (bool, error) { return true, nil },
		Reject: func(w http.ResponseWriter, _ *http.Request, err error) {
			if appErr, ok := errors.AsType[*contract.AppError](err); ok {
				w.WriteHeader(appErr.Status)

				return
			}

			w.WriteHeader(http.StatusInternalServerError)
		},
	}
}

func TestStreamsRequireCompletePolicy(t *testing.T) {
	for _, missing := range []string{"authorize", "family", "reject", "origin", "mode"} {
		t.Run(missing, func(t *testing.T) {
			policy := streamTestPolicy()

			switch missing {
			case "authorize":
				policy.Authorize = nil
			case "family":
				policy.FamilyActive = nil
			case "reject":
				policy.Reject = nil
			case "origin":
				policy.AllowedOrigins = nil
			case "mode":
				policy.OriginMode = "invalid"
			}

			_, err := NewStreams(NewHub(nil), slog.New(slog.DiscardHandler), policy, DefaultStreamTiming())
			require.Error(t, err)
		})
	}
}

func TestStreamsGenerationAndOriginRejectBeforeAuthorization(t *testing.T) {
	var calls atomic.Int32

	policy := streamTestPolicy()

	policy.Authorize = func(*http.Request) (string, error) {
		calls.Add(1)

		return "family", nil
	}

	streams, err := NewStreams(NewHub(nil), slog.New(slog.DiscardHandler), policy, DefaultStreamTiming())
	require.NoError(t, err)
	t.Cleanup(streams.Close)

	for _, tc := range []struct {
		protocol, origin string
		status           int
	}{
		{"", streamTestOrigin, 409},
		{"admin-stats.old", streamTestOrigin, 409},
		{StatsProtocol() + ", other", streamTestOrigin, 409},
		{StatsProtocol(), "https://untrusted.test", 403},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
		req.Header.Set("Sec-WebSocket-Protocol", tc.protocol)
		req.Header.Set("Origin", tc.origin)

		rec := httptest.NewRecorder()
		streams.ServeHTTP(rec, req)
		require.Equal(t, tc.status, rec.Code)
	}

	require.Zero(t, calls.Load())
}

func TestStreamsCloseJoinsPeerFamilyCheckAndSubscription(t *testing.T) {
	hub := NewHub(nil)
	hub.Publish(&SystemStats{})

	checking, canceled := make(chan struct{}), make(chan struct{})
	policy := streamTestPolicy()

	policy.FamilyActive = func(ctx context.Context, _ string) (bool, error) {
		close(checking)
		<-ctx.Done()
		close(canceled)

		return false, ctx.Err()
	}

	streams, err := NewStreams(hub, slog.New(slog.DiscardHandler), policy, DefaultStreamTiming())
	require.NoError(t, err)

	server := httptest.NewServer(streams)
	t.Cleanup(server.Close)
	t.Cleanup(streams.Close)

	dialer := websocket.Dialer{Subprotocols: []string{StatsProtocol()}, HandshakeTimeout: time.Second}
	conn, resp, err := dialer.DialContext(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), http.Header{"Origin": {streamTestOrigin}})
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	t.Cleanup(func() { closeConn(conn) })
	require.Equal(t, StatsProtocol(), conn.Subprotocol())
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))

	_, _, err = conn.ReadMessage()
	require.NoError(t, err, "negotiated connection must deliver history")

	select {
	case <-checking:
	case <-time.After(2 * time.Second):
		t.Fatal("family check did not start")
	}

	streams.Close()

	select {
	case <-canceled:
	default:
		t.Fatal("Close returned before the family check exited")
	}

	require.False(t, hub.hasSubscribers())

	_, _, err = conn.ReadMessage()
	require.Error(t, err)

	if networkErr, ok := errors.AsType[net.Error](err); ok {
		require.False(t, networkErr.Timeout(), "peer must close, not time out")
	}

	_, resp, err = dialer.DialContext(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), http.Header{"Origin": {streamTestOrigin}})
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}
