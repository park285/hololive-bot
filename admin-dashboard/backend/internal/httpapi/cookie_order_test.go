package httpapi

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/session"
)

func TestDelayedUnauthorizedResponseCannotClearNewLoginCookie(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	releaseRequest := sync.OnceFunc(func() { close(release) })

	defer releaseRequest()

	client, baseURL := cookieOrderClient(t, entered, release)

	old := cookieOrderRequest(t, baseURL, http.MethodGet, "/admin/api/holo/rooms", "")
	old.AddCookie(signedSessionCookie("old-session"))

	completed := make(chan *http.Response, 1)

	go func() {
		response, callErr := client.Do(old)
		if callErr != nil {
			t.Errorf("old request: %v", callErr)
		} else if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close old response: %v", closeErr)
		}

		completed <- response
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		releaseRequest()
		t.Fatal("old request did not enter")
	}

	login, err := client.Do(cookieOrderRequest(t, baseURL, http.MethodPost, "/admin/api/auth/login", `{"username":"admin","password":"correct-password"}`))
	require.NoError(t, err)
	require.NotNil(t, login)
	require.Equal(t, http.StatusOK, login.StatusCode)
	require.NoError(t, login.Body.Close())
	releaseRequest()

	previous := <-completed
	require.NotNil(t, previous)
	require.Equal(t, http.StatusUnauthorized, previous.StatusCode)
	require.Empty(t, previous.Cookies(), "late rejection must not change either auth cookie")

	assertNewSessionCookie(t, client.Jar.Cookies(login.Request.URL), newLoginSessionID)

	status, err := client.Do(cookieOrderRequest(t, baseURL, http.MethodGet, "/admin/api/auth/session", ""))
	require.NoError(t, err)
	require.NotNil(t, status)
	require.Equal(t, http.StatusOK, status.StatusCode)
	require.NoError(t, status.Body.Close())
}

func assertNewSessionCookie(t *testing.T, cookies []*http.Cookie, id string) {
	t.Helper()

	for _, cookie := range cookies {
		if cookie.Name == auth.SessionCookieName {
			require.Equal(t, auth.SignSessionID(id, testSecret), cookie.Value)

			return
		}
	}

	t.Fatal("new login cookie is missing")
}

func cookieOrderRequest(t *testing.T, baseURL, method, path, body string) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, baseURL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Client-Generation", contract.Generation)

	return req
}

const newLoginSessionID = "new-login-session"

func cookieOrderClient(t *testing.T, entered chan struct{}, release <-chan struct{}) (*http.Client, string) {
	t.Helper()

	current := liveSession(newLoginSessionID)
	store := storeWith(current)

	store.createFn = func(context.Context) (session.Session, error) { return *current, nil }

	lookup := store.getFn

	store.getFn = func(ctx context.Context, id string) (*session.Session, error) {
		if id == "old-session" {
			close(entered)
			<-release
		}

		return lookup(ctx, id)
	}

	r := newTestRuntime(t, store, nil)
	server := httptest.NewServer(r.Handler())
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	client := server.Client()

	client.Jar = jar

	return client, server.URL
}
