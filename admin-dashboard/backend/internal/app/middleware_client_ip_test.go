package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/config"
)

func TestClientIPXFFHostPortHopIgnored(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, func(cfg *config.Config) {
		cfg.TrustedForwarders = true
		cfg.TrustedProxyCIDRs = append(cfg.TrustedProxyCIDRs, mustCIDR(t))
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", http.NoBody)
	req.RemoteAddr = "10.0.0.9:4321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7:1234, 203.0.113.8, 10.0.0.1")

	require.Equal(t, "203.0.113.8", rt.clientIP(req))
}

func TestClientIPXRealIPHostPortIgnored(t *testing.T) {
	rt := newTestRuntime(t, &fakeSessions{}, func(cfg *config.Config) {
		cfg.TrustedForwarders = true
		cfg.TrustedProxyCIDRs = append(cfg.TrustedProxyCIDRs, mustCIDR(t))
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", http.NoBody)
	req.RemoteAddr = "10.0.0.9:4321"
	req.Header.Set("X-Real-IP", "203.0.113.7:1234")

	require.Equal(t, "10.0.0.9", rt.clientIP(req))
}
