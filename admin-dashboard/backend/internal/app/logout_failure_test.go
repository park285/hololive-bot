package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kapu/admin-dashboard/internal/auth"
)

func TestLogoutDoesNotReportRevocationOnStoreError(t *testing.T) {
	store := storeWith(liveSession("audit-session"))

	store.revokeFn = func(context.Context, string) error {
		return errors.New("synthetic session delete failure")
	}

	runtime := newTestRuntime(t, store, nil)

	csrf, err := auth.NewCSRFToken("audit-session", testSecret)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/api/auth/logout", http.NoBody)
	request.AddCookie(signedSessionCookie("audit-session"))
	request.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: csrf, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	request.Header.Set("X-CSRF-Token", csrf)

	response := doRequest(runtime.Handler(), request)
	probe := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/api/auth/session", http.NoBody)
	probe.AddCookie(signedSessionCookie("audit-session"))

	stillAuthenticated := doRequest(runtime.Handler(), probe)
	if response.Code != http.StatusServiceUnavailable || stillAuthenticated.Code != http.StatusOK || !clearsAuthCookies(response) {
		t.Fatalf("logout=%d, remaining server session=%d, cookies cleared=%t", response.Code, stillAuthenticated.Code, clearsAuthCookies(response))
	}
}
