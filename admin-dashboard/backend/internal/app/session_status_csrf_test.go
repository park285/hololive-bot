package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/auth"
)

func csrfCookieFrom(rec *httptest.ResponseRecorder) (*http.Cookie, bool) {
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == auth.CSRFCookieName {
			return cookie, true
		}
	}
	return nil, false
}

func sessionStatusRequest(sessionID, csrf string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/api/auth/session", http.NoBody)
	req.AddCookie(signedSessionCookie(sessionID))
	if csrf != "" {
		req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: csrf, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	}
	return req
}

func TestSessionStatusDuringRotationGraceDoesNotRewriteCSRFCookie(t *testing.T) {
	store := storeWithSessions(rotatedMarker("marker-session", "replacement-session"), liveSession("replacement-session"))
	rt := newTestRuntime(t, store, nil)

	markerCSRF, err := auth.NewCSRFToken("marker-session", testSecret)
	require.NoError(t, err)

	rec := doRequest(rt.Handler(), sessionStatusRequest("marker-session", markerCSRF))

	require.Equal(t, http.StatusOK, rec.Code)
	_, written := csrfCookieFrom(rec)
	require.False(t, written,
		"echoing the client's own CSRF token must not Set-Cookie; a concurrent heartbeat rotation may have just written a replacement-bound token")
}

func TestSessionStatusIssuesCSRFCookieWhenClientHasNone(t *testing.T) {
	store := storeWith(liveSession("plain-session"))
	rt := newTestRuntime(t, store, nil)

	rec := doRequest(rt.Handler(), sessionStatusRequest("plain-session", ""))

	require.Equal(t, http.StatusOK, rec.Code)
	cookie, written := csrfCookieFrom(rec)
	require.True(t, written, "a client without a CSRF cookie must receive one")
	require.True(t, auth.ValidateCSRFToken("plain-session", cookie.Value, testSecret))
}

func TestSessionStatusReplacesAnInvalidCSRFCookie(t *testing.T) {
	store := storeWith(liveSession("plain-session"))
	rt := newTestRuntime(t, store, nil)

	rec := doRequest(rt.Handler(), sessionStatusRequest("plain-session", "not-a-valid-token"))

	require.Equal(t, http.StatusOK, rec.Code)
	cookie, written := csrfCookieFrom(rec)
	require.True(t, written, "an invalid CSRF cookie must be replaced")
	require.True(t, auth.ValidateCSRFToken("plain-session", cookie.Value, testSecret))
}
