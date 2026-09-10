package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/session"
)

func TestContractGenerationRejectsBeforeProtectedWork(t *testing.T) {
	lookups := 0
	rt := newTestRuntime(t, &fakeSessions{getFn: func(context.Context, string) (*session.Session, error) {
		lookups++
		return liveSession("fixture-session"), nil
	}}, nil)
	t.Cleanup(rt.Close)

	for _, generation := range []string{"", "old-client", strings.Repeat("a", 64)} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/api/holo/settings", strings.NewReader(`{"alarmAdvanceMinutes":5}`))
		req.AddCookie(signedSessionCookie("fixture-session"))
		req.Header.Set("X-Admin-Client-Generation", generation)

		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, req)
		require.Equal(t, http.StatusConflict, rec.Code)
		require.Equal(t, "CLIENT_GENERATION_MISMATCH", decodeBody(t, rec)["code"])
		require.NotEmpty(t, decodeBody(t, rec)["requestId"])
		require.Equal(t, contract.Generation, rec.Header().Get("X-Admin-Server-Generation"))
	}

	require.Zero(t, lookups)
}

func TestContractGenerationMetadataCannotBecome304(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)
	t.Cleanup(rt.Close)

	for range 2 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/meta.json", http.NoBody)
		req.Header.Set("If-None-Match", "*")

		rec := httptest.NewRecorder()
		rt.Handler().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Header().Get("Cache-Control"), "no-store")
		require.Empty(t, rec.Header().Get("ETag"))
		require.Equal(t, contract.Generation, decodeBody(t, rec)["clientGeneration"])
	}
}
