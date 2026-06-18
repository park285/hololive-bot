package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAuthCookiesUseStrictSecurityAttributes(t *testing.T) {
	rec := httptest.NewRecorder()

	SetSessionCookie(rec, "signed-session", time.Hour, true)
	SetCSRFCookie(rec, "csrf-token", true)
	ClearAuthCookies(rec, true)

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 4)

	sessionCookie := cookies[0]
	require.Equal(t, SessionCookieName, sessionCookie.Name)
	require.True(t, sessionCookie.Secure)
	require.True(t, sessionCookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, sessionCookie.SameSite)

	csrfCookie := cookies[1]
	require.Equal(t, CSRFCookieName, csrfCookie.Name)
	require.True(t, csrfCookie.Secure)
	require.True(t, csrfCookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, csrfCookie.SameSite)

	clearedSessionCookie := cookies[2]
	require.Equal(t, SessionCookieName, clearedSessionCookie.Name)
	require.True(t, clearedSessionCookie.Secure)
	require.True(t, clearedSessionCookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, clearedSessionCookie.SameSite)

	clearedCSRFCookie := cookies[3]
	require.Equal(t, CSRFCookieName, clearedCSRFCookie.Name)
	require.True(t, clearedCSRFCookie.Secure)
	require.True(t, clearedCSRFCookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, clearedCSRFCookie.SameSite)
}

func TestAuthCookiesHonorInsecureDevelopmentMode(t *testing.T) {
	rec := httptest.NewRecorder()

	SetSessionCookie(rec, "signed-session", time.Hour, false)
	SetCSRFCookie(rec, "csrf-token", false)
	ClearAuthCookies(rec, false)

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 4)
	for _, cookie := range cookies {
		require.False(t, cookie.Secure, "cookie %s Secure", cookie.Name)
	}
}
