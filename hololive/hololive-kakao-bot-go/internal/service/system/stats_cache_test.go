// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package system

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

func TestNewCollector_DefaultConfiguration(t *testing.T) {
	endpoints := []ServiceEndpoint{{Name: "svc-a", URL: "http://example.com/health"}}

	collector := NewCollector(endpoints)
	if collector == nil {
		t.Fatal("NewCollector returned nil")
	}

	if collector.httpClient == nil {
		t.Fatal("httpClient should be initialized")
	}

	if collector.httpClient.Timeout != 2*time.Second {
		t.Fatalf("timeout=%s want=2s", collector.httpClient.Timeout)
	}

	if collector.httpClient.Transport == nil {
		t.Fatal("transport should be initialized")
	}

	if collector.cacheTTL != 2*time.Second {
		t.Fatalf("cacheTTL=%s want=2s", collector.cacheTTL)
	}

	if len(collector.endpoints) != 1 || collector.endpoints[0].Name != "svc-a" {
		t.Fatalf("unexpected endpoints: %+v", collector.endpoints)
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

	stats := collector.fetchServiceGoroutines(t.Context())
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
	collector := NewCollector(nil)

	first, err := collector.GetCurrentStats(t.Context())
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

	second, err := collector.GetCurrentStats(t.Context())
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
