package httpapi

import (
	jsonv2 "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/adapters/holo"
	"github.com/kapu/admin-dashboard/internal/auth"
)

const (
	mutationSessionID = "typed-mutation-session"
	holoSuccessBody   = `{"status":"ok"}`
)

func mutationRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()

	token, err := auth.NewCSRFToken(mutationSessionID, testSecret)
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Mutation-ID", newMutationID())
	req.Header.Set("X-CSRF-Token", token)
	req.AddCookie(signedSessionCookie(mutationSessionID))
	req.AddCookie(&http.Cookie{Name: auth.CSRFCookieName, Value: token, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})

	return req
}

func runtimeWithHolo(t *testing.T, handler http.Handler) *API {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := holo.NewClient(server.URL, "synthetic-api-key")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	r := newTestRuntime(t, storeWith(liveSession(mutationSessionID)), nil)

	r.holo = client

	return r
}

func TestTypedHoloMutationsMatchOwnedUpstreamAndDispatchOnce(t *testing.T) {
	tests := []struct {
		method, path, body, reply string
		upstreamStatus            int
	}{
		{http.MethodPost, "/holo/members", `{"name":"한글","channelId":"UC-fixture","aliases":{"ko":[],"ja":[]},"isGraduated":false}`, holoSuccessBody, 201},
		{http.MethodPost, "/holo/members/9007199254740993/aliases", `{"type":"ko","alias":"한글 별명"}`, holoSuccessBody, 200},
		{http.MethodDelete, "/holo/members/9007199254740993/aliases", `{"type":"ja","alias":"別名"}`, holoSuccessBody, 200},
		{http.MethodPatch, "/holo/members/9007199254740993/graduation", `{"isGraduated":false}`, holoSuccessBody, 200},
		{http.MethodPatch, "/holo/members/9007199254740993/channel", `{"channelId":"UC-fixture"}`, holoSuccessBody, 200},
		{http.MethodPatch, "/holo/members/9007199254740993/name", `{"name":"한글 멤버"}`, holoSuccessBody, 200},
		{http.MethodPost, "/holo/rooms", `{"room":"9007199254740993"}`, holoSuccessBody, 200},
		{http.MethodDelete, "/holo/rooms", `{"room":"9007199254740993"}`, holoSuccessBody, 200},
		{http.MethodPost, "/holo/rooms/acl", `{"enabled":false,"mode":"blacklist"}`, `{"status":"ok","enabled":false,"mode":"blacklist","message":"ignored"}`, 200},
		{http.MethodDelete, "/holo/alarms", `{"roomId":"9007199254740993","channelId":"UC-fixture"}`, `{"status":"ok","removed":false}`, 200},
		{http.MethodPost, "/holo/names/room", `{"roomId":"9007199254740993","roomName":"한글 방"}`, holoSuccessBody, 200},
		{http.MethodPost, "/holo/names/user", `{"userId":"9007199254740993","userName":"한글 사용자"}`, holoSuccessBody, 200},
		{http.MethodPost, "/holo/settings", `{"alarmAdvanceMinutes":15}`, `{"status":"ok","settings":{"alarmAdvanceMinutes":15,"scraperProxyEnabled":true},"runtime":{"alarm_applied":false,"config_publish_alarm_advance_minutes":false,"config_publish_alarm_advance_minutes_error":"synthetic-private-error"}}`, 200},
	}
	for _, tc := range tests {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			var calls atomic.Int32

			r := runtimeWithHolo(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)

				if req.Method != tc.method || req.URL.Path != "/api"+tc.path || req.URL.RawQuery != "" {
					t.Error("unexpected upstream method/path")
				}

				assertMutationBody(t, req, tc.body)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.upstreamStatus)

				if _, err := w.Write([]byte(tc.reply)); err != nil {
					t.Errorf("write fixture: %v", err)
				}
			}))
			rec := doRequest(r.Handler(), mutationRequest(t, tc.method, "/admin/api"+tc.path, tc.body))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, int32(1), calls.Load())
			require.NotContains(t, rec.Body.String(), "synthetic-private-error")
			require.NotContains(t, rec.Body.String(), "scraperProxyEnabled")

			if tc.path == "/holo/settings" {
				require.Contains(t, rec.Body.String(), `"alarm_applied":false`)
				require.Contains(t, rec.Body.String(), `"config_publish_alarm_advance_minutes":false`)
			}
		})
	}
}

func TestInvalidHoloMutationsCannotReachUpstream(t *testing.T) {
	var calls atomic.Int32

	r := runtimeWithHolo(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/holo/members", `{"id":1,"name":"name","channelId":"UC","aliases":{"ko":[],"ja":[]},"isGraduated":false}`},
		{http.MethodPost, "/holo/members", `{"name":"name","channelId":"UC","aliases":{"ko":[null],"ja":[]},"isGraduated":false}`},
		{http.MethodPatch, "/holo/members/9223372036854775808/name", `{"name":"name"}`},
		{http.MethodPatch, "/holo/members/1/graduation", `{"isGraduated":null}`},
		{http.MethodPost, "/holo/members/1/aliases", `{"type":"ko","alias":" "}`},
		{http.MethodPost, "/holo/rooms/acl", `{}`},
		{http.MethodPost, "/holo/rooms/acl", `{"enabled":true,"mode":"invalid"}`},
		{http.MethodPost, "/holo/settings", `{"alarmAdvanceMinutes":1441}`},
		{http.MethodPost, "/holo/settings", `{"alarmAdvanceMinutes":15,"scraperProxyEnabled":true}`},
		{http.MethodPost, "/holo/rooms?unowned=1", `{"room":"1"}`},
		{http.MethodPost, "/holo/names/user", `{"userId":"1","userName":null}`},
	} {
		rec := doRequest(r.Handler(), mutationRequest(t, tc.method, "/admin/api"+tc.path, tc.body))
		require.Equal(t, http.StatusBadRequest, rec.Code, tc.path)
	}

	require.Zero(t, calls.Load())
}

func assertMutationBody(t *testing.T, req *http.Request, expectedBody string) {
	t.Helper()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Errorf("read fixture body: %v", err)

		return
	}

	var actual, expected any

	if err := jsonv2.Unmarshal(body, &actual); err != nil {
		t.Errorf("decode body: %v", err)

		return
	}

	if err := jsonv2.Unmarshal([]byte(expectedBody), &expected); err != nil {
		t.Errorf("decode expected: %v", err)

		return
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Error("upstream input changed")
	}
}
