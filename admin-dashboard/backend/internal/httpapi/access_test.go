package httpapi

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/contract"
)

func TestEndpointRegistrationRejectsIncompleteOrWeakerAccess(t *testing.T) {
	r := newTestRuntime(t, &fakeSessions{}, nil)
	routes := r.endpoints()
	require.NoError(t, validateEndpoints(routes))

	for _, mutate := range []func([]endpoint) []endpoint{
		func(e []endpoint) []endpoint { return e[1:] },
		func(e []endpoint) []endpoint { e[2].access = publicLogin; return e },
		func(e []endpoint) []endpoint { e[2].access = ""; return e },
		func(e []endpoint) []endpoint { e[2].method = http.MethodPost; return e },
		func(e []endpoint) []endpoint { e[2].handler = nil; return e },
		func(e []endpoint) []endpoint { return append(e, e[0]) },
	} {
		require.Error(t, validateEndpoints(mutate(slices.Clone(routes))))
	}
}

func TestReservedMissingPathsAndMethodErrorsNeverBecomeSPA(t *testing.T) {
	r := newTestRuntime(t, &fakeSessions{}, nil)

	for _, target := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/admin/api", http.StatusNotFound},
		{http.MethodGet, "/admin/api/missing", http.StatusNotFound},
		{http.MethodGet, "/assets", http.StatusNotFound},
		{http.MethodGet, "/missing.js", http.StatusNotFound},
		{http.MethodDelete, "/admin/api/auth/session", http.StatusMethodNotAllowed},
	} {
		req := httptest.NewRequestWithContext(t.Context(), target.method, target.path, http.NoBody)
		req.Header.Set("X-Admin-Client-Generation", contract.Generation)

		rec := doRequest(r.Handler(), req)
		require.Equal(t, target.status, rec.Code)
		require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
		require.Equal(t, adminDashboardCSP, rec.Header().Get("Content-Security-Policy"))

		var body contract.ErrorResponse

		require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &body))
		require.NotEmpty(t, body.Code)
		require.NotEmpty(t, body.RequestID)
	}
}

func TestPanicResponseDoesNotDumpSecrets(t *testing.T) {
	r := newTestRuntime(t, &fakeSessions{}, nil)

	var logs bytes.Buffer

	r.logger = slog.New(slog.NewTextHandler(&logs, nil))

	engine, ok := r.Handler().(*gin.Engine)
	require.True(t, ok)
	engine.GET("/admin/api/panic-fixture", func(*gin.Context) { panic("synthetic-private-panic") })

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/api/panic-fixture", http.NoBody)
	req.Header.Set("Cookie", "synthetic-private-cookie")

	rec := doRequest(engine, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.NotContains(t, logs.String()+rec.Body.String(), "synthetic-private")
	require.Contains(t, logs.String(), "request_id=")
}
