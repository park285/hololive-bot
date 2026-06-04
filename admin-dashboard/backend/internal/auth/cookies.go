package auth

import (
	"net/http"
	"time"
)

const (
	SessionCookieName = "admin_session"
	CSRFCookieName    = "csrf_token"
)

func SetSessionCookie(w http.ResponseWriter, value string, maxAge time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func SetCSRFCookie(w http.ResponseWriter, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func ClearAuthCookies(w http.ResponseWriter, secure bool) {
	clearCookie(w, SessionCookieName, true, secure)
	clearCookie(w, CSRFCookieName, false, secure)
}

func clearCookie(w http.ResponseWriter, name string, httpOnly, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: httpOnly,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}
