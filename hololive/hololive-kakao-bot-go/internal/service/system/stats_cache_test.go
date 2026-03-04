package system

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewCollector_DefaultConfiguration(t *testing.T) {
	endpoints := []ServiceEndpoint{{Name: "svc-a", URL: "http://example.com/health"}}

	collector := NewCollector(endpoints, false)
	if collector == nil {
		t.Fatal("NewCollector returned nil")
	}
	if collector.httpClient == nil {
		t.Fatal("httpClient should be initialized")
	}
	if collector.httpClient.Timeout != 2*time.Second {
		t.Fatalf("timeout=%s want=2s", collector.httpClient.Timeout)
	}
	if collector.cacheTTL != 2*time.Second {
		t.Fatalf("cacheTTL=%s want=2s", collector.cacheTTL)
	}
	if len(collector.endpoints) != 1 || collector.endpoints[0].Name != "svc-a" {
		t.Fatalf("unexpected endpoints: %+v", collector.endpoints)
	}

	collectorOTel := NewCollector(nil, true)
	if collectorOTel == nil || collectorOTel.httpClient == nil {
		t.Fatal("NewCollector(enableOTel=true) should initialize client")
	}
}

func TestCollector_GetCachedStats_CloneAndExpiry(t *testing.T) {
	collector := &Collector{cacheTTL: 2 * time.Second}
	collector.cached = &SystemStats{
		CPUUsage: 10,
		ServiceGoroutines: []ServiceGoroutines{{
			Name:       "svc-a",
			Goroutines: 7,
			Available:  true,
		}},
	}
	collector.cachedAt = time.Now()

	cached := collector.getCachedStats()
	if cached == nil {
		t.Fatal("expected cached stats")
	}
	if cached == collector.cached {
		t.Fatal("getCachedStats should return cloned value")
	}

	cached.ServiceGoroutines[0].Goroutines = 999
	if collector.cached.ServiceGoroutines[0].Goroutines == 999 {
		t.Fatal("mutating returned stats should not affect cache")
	}

	collector.cachedAt = time.Now().Add(-3 * time.Second)
	if got := collector.getCachedStats(); got != nil {
		t.Fatalf("expected expired cache to return nil, got %+v", got)
	}
}

func TestCollector_FetchServiceGoroutines_MixedEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"goroutines": 11})
	}))
	defer srv.Close()

	collector := &Collector{
		httpClient: srv.Client(),
		endpoints: []ServiceEndpoint{
			{Name: "empty-url", URL: ""},
			{Name: "healthy", URL: srv.URL},
		},
	}

	stats := collector.fetchServiceGoroutines(context.Background())
	if len(stats) != 2 {
		t.Fatalf("len(stats)=%d want=2", len(stats))
	}

	if stats[0].Name != "empty-url" || stats[0].Available {
		t.Fatalf("unexpected empty-url result: %+v", stats[0])
	}
	if stats[1].Name != "healthy" || !stats[1].Available || stats[1].Goroutines != 11 {
		t.Fatalf("unexpected healthy result: %+v", stats[1])
	}
}

func TestCollector_GetCurrentStats_CacheMissThenHit(t *testing.T) {
	collector := NewCollector(nil, false)

	first, err := collector.GetCurrentStats(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentStats(first) error: %v", err)
	}
	if first == nil {
		t.Fatal("first stats is nil")
	}
	if first.Goroutines <= 0 {
		t.Fatalf("first.Goroutines=%d want>0", first.Goroutines)
	}
	if len(first.ServiceGoroutines) == 0 || first.ServiceGoroutines[0].Name != "hololive-bot" {
		t.Fatalf("unexpected service goroutines: %+v", first.ServiceGoroutines)
	}
	if collector.cached == nil {
		t.Fatal("collector.cached should be populated after cache miss path")
	}

	cachedAtBefore := collector.cachedAt
	second, err := collector.GetCurrentStats(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentStats(second) error: %v", err)
	}
	if second == nil {
		t.Fatal("second stats is nil")
	}
	if !collector.cachedAt.Equal(cachedAtBefore) {
		t.Fatalf("cachedAt should remain same on cache hit: before=%s after=%s", cachedAtBefore, collector.cachedAt)
	}
	if second == collector.cached {
		t.Fatal("cache hit should return clone, not cached pointer")
	}
}
