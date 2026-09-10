package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/session"
)

func TestMutationInputAndStoreFailureCannotDispatch(t *testing.T) {
	var calls atomic.Int32

	r := runtimeWithHolo(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	for _, id := range []string{"", "123", "5ae58f70-51c4-1df2-bf70-683773e40638"} {
		req := mutationRequest(t, http.MethodPost, "/admin/api/holo/rooms", `{"room":"fixture"}`)
		req.Header.Set("X-Admin-Client-Generation", contract.Generation)
		req.Header.Set("X-Admin-Mutation-ID", id)

		rec := httptest.NewRecorder()
		r.Handler().ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.NotContains(t, rec.Body.String(), "notDispatchedMutationId")
	}

	store := storeWith(liveSession(mutationSessionID))

	store.claimFn = func(context.Context, session.Session, string) (bool, error) {
		return false, errors.New("synthetic failure")
	}
	r.sessions = store

	rec := doRequest(r.Handler(), mutationRequest(t, http.MethodPost, "/admin/api/holo/rooms", `{"room":"fixture"}`))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "MUTATION_ADMISSION_UNAVAILABLE")
	require.NotContains(t, rec.Body.String(), "notDispatchedMutationId")
	require.EqualValues(t, 0, calls.Load())
}

func TestOnlyFirstClaimBeforeDispatchCanProveRejection(t *testing.T) {
	var effects atomic.Int32

	r := runtimeWithHolo(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		effects.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	id := newMutationID()
	request := func(body string) *http.Request {
		req := mutationRequest(t, http.MethodPost, "/admin/api/holo/rooms", body)
		req.Header.Set("X-Admin-Mutation-ID", id)

		return req
	}
	first := doRequest(r.Handler(), request(`{"room":""}`))
	require.Equal(t, http.StatusBadRequest, first.Code)
	require.Equal(t, id, decodeBody(t, first)["notDispatchedMutationId"])

	duplicate := doRequest(r.Handler(), request(`{"room":"valid"}`))
	require.Equal(t, http.StatusConflict, duplicate.Code)
	require.NotContains(t, duplicate.Body.String(), "notDispatchedMutationId")
	require.EqualValues(t, 0, effects.Load())

	id = newMutationID()

	attempted := doRequest(r.Handler(), request(`{"room":"valid"}`))
	require.Equal(t, http.StatusBadGateway, attempted.Code)
	require.NotContains(t, attempted.Body.String(), "notDispatchedMutationId")
	require.EqualValues(t, 1, effects.Load())

	for _, denied := range []string{"auth", "csrf", "drain"} {
		req := request(`{"room":"valid"}`)

		switch denied {
		case "auth":
			req.Header.Del("Cookie")
		case "csrf":
			req.Header.Del("X-CSRF-Token")
		case "drain":
			r.BeginDrain()
		}

		rec := doRequest(r.Handler(), req)
		require.GreaterOrEqual(t, rec.Code, 400)
		require.NotContains(t, rec.Body.String(), "notDispatchedMutationId", denied)
	}

	require.EqualValues(t, 1, effects.Load())
}

func TestRealBFFDeduplicatesConcurrentAndLostResponseEffects(t *testing.T) {
	var effects atomic.Int32

	r := runtimeWithHolo(t, lostEffectHandler(t, &effects))
	mr := miniredis.RunT(t)
	store, err := session.NewStoreWithOptions(t.Context(), mr.Addr(), &r.cfg.Session, session.Options{DisableCache: true, ForceSingleClient: true})
	require.NoError(t, err)
	t.Cleanup(store.Close)

	r.sessions = newCleanupSessionStore(store)

	sess, err := store.Create(t.Context())
	require.NoError(t, err)

	token, err := auth.NewCSRFToken(sess.ID, testSecret)
	require.NoError(t, err)

	server := httptest.NewServer(r.Handler())
	t.Cleanup(server.Close)

	statuses := concurrentMutationStatuses(t, server, sess, token)

	require.Equal(t, map[int]int{http.StatusBadGateway: 1, http.StatusConflict: 15}, statuses)
	require.EqualValues(t, 1, effects.Load(), "all HTTP attempts and methods share one family claim")
}

func lostEffectHandler(t *testing.T, effects *atomic.Int32) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, err := io.Copy(io.Discard, req.Body); err != nil {
			t.Error(err)

			return
		}

		effects.Add(1)

		// 실제 upstream이 효과 이후 연결을 잃어도 claim을 해제할 수 없습니다.
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("missing test HTTP hijacker")

			return
		}

		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)

			return
		}

		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	})
}

func concurrentMutationStatuses(t *testing.T, server *httptest.Server, sess session.Session, token string) map[int]int {
	t.Helper()

	id := newMutationID()
	results := make(chan int, 16)
	errs := make(chan error, 16)

	var wg sync.WaitGroup

	for index := range 16 {
		req := mutationRequest(t, http.MethodPost, "/admin/api/holo/rooms", `{"room":"fixture"}`)

		if index%2 == 1 {
			req.Method = http.MethodDelete
		}

		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		req.RequestURI = ""
		req.Header.Del("Cookie")
		req.AddCookie(signedSessionCookie(sess.ID))
		req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: token, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		req.Header.Set("X-CSRF-Token", token)
		req.Header.Set("X-Admin-Client-Generation", contract.Generation)
		req.Header.Set("X-Admin-Mutation-ID", id)
		wg.Go(func() {
			response, callErr := server.Client().Do(req)
			if callErr != nil {
				errs <- callErr
				return
			}

			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()

			if err := errors.Join(readErr, closeErr); err != nil {
				errs <- err
				return
			}

			results <- response.StatusCode
		})
	}

	wg.Wait()
	close(results)
	close(errs)

	for callErr := range errs {
		require.NoError(t, callErr)
	}

	statuses := make(map[int]int)

	for status := range results {
		statuses[status]++
	}

	return statuses
}
