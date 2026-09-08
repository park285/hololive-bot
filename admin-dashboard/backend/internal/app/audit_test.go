package app

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/config"
)

func TestAdminMutationAuditIncludesRequestActorTargetAndResult(t *testing.T) {
	sess := liveSession("audit-mutation-session")
	rt := newTestRuntime(t, storeWith(sess), func(cfg *config.Config) {
		cfg.Security.CSRFMode = config.SecurityOff
	})

	var output bytes.Buffer

	rt.logger = slog.New(slog.NewJSONHandler(&output, nil))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/api/docker/containers/hololive-api/restart", http.NoBody)

	req.RemoteAddr = "192.0.2.15:43210"

	req.AddCookie(signedSessionCookie(sess.ID))

	rec := doRequest(rt.Handler(), req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var record map[string]any

	require.NoError(t, jsonv2.Unmarshal(output.Bytes(), &record), "log: %s", output.String())
	require.Equal(t, "admin.mutation", record["event"])
	require.NotEmpty(t, record["request_id"])
	require.Equal(t, "admin", record["actor"])
	require.Equal(t, "192.0.2.15", record["client_ip"])
	require.Equal(t, "/admin/api/docker/containers/:name/restart", record["route"])
	require.Equal(t, "hololive-api", record["target"])
	require.Equal(t, "failed", record["result"])
	require.EqualValues(t, http.StatusServiceUnavailable, record["status"])
}

func TestAuthenticationRejectionIsAudited(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, nil)

	var output bytes.Buffer

	rt.logger = slog.New(slog.NewJSONHandler(&output, nil))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/api/docker/containers/hololive-api/restart", http.NoBody)

	req.RemoteAddr = "198.51.100.9:54321"

	rec := doRequest(rt.Handler(), req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)

	var record map[string]any

	require.NoError(t, jsonv2.Unmarshal(output.Bytes(), &record), "log: %s", output.String())
	require.Equal(t, "admin.authentication.denied", record["event"])
	require.Equal(t, "denied", record["result"])
	require.NotEmpty(t, record["request_id"])
}
