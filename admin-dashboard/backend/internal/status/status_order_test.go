package status

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCollectorPreservesConfiguredEndpointOrder(t *testing.T) {
	slow := newDelayedHealthServer(t, 80*time.Millisecond, 11)
	fast := newDelayedHealthServer(t, 0, 22)
	collector := NewCollector([]ServiceEndpoint{
		{Name: "slow", URL: slow.URL, HealthPath: "/health"},
		{Name: "fast", URL: fast.URL, HealthPath: "/health"},
	}, "test")

	result := collector.Collect(t.Context())
	names := make([]string, len(result.Services))
	for i := range result.Services {
		names[i] = result.Services[i].Name
	}

	require.Equal(t, []string{"admin-dashboard", "slow", "fast"}, names)
}

func TestHubPreservesConfiguredEndpointOrder(t *testing.T) {
	slow := newDelayedHealthServer(t, 80*time.Millisecond, 11)
	fast := newDelayedHealthServer(t, 0, 22)
	hub := NewHub([]ServiceEndpoint{
		{Name: "slow", URL: slow.URL, HealthPath: "/health"},
		{Name: "fast", URL: fast.URL, HealthPath: "/health"},
	})

	result := hub.externalRuntimeStats(t.Context())
	names := make([]string, len(result))
	counts := make([]int, len(result))
	for i := range result {
		names[i] = result[i].Name
		counts[i] = result[i].Count
	}

	require.Equal(t, []string{"slow", "fast"}, names)
	require.Equal(t, []int{11, 22}, counts)
}

func newDelayedHealthServer(t *testing.T, delay time.Duration, goroutines int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"status":"ok","goroutines":` + strconv.Itoa(goroutines) + `}`)); err != nil {
			t.Errorf("write health response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
