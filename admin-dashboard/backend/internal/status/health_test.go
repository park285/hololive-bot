package status

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/json"
)

func newStubCollector(name, healthPath string, srv *httptest.Server) *Collector {
	endpoint := ServiceEndpoint{Name: name, URL: srv.URL, HealthPath: healthPath}
	c := &Collector{
		clients:   map[string]endpointClient{name: {client: srv.Client()}},
		endpoints: []ServiceEndpoint{endpoint},
		start:     time.Now(),
		version:   "test",
	}
	return c
}

func newStubHub(name, healthPath string, srv *httptest.Server) *Hub {
	endpoint := ServiceEndpoint{Name: name, URL: srv.URL, HealthPath: healthPath}
	return &Hub{
		endpoints: []ServiceEndpoint{endpoint},
		clients:   map[string]endpointClient{name: {client: srv.Client()}},
	}
}

func TestCollectEndpointSuccess(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newStubCollector("svc", "/health", srv)
	status := c.collectEndpoint(t.Context(), c.endpoints[0])

	if gotPath != "/health" {
		t.Fatalf("requested path = %q, want /health", gotPath)
	}
	if !status.Available {
		t.Fatalf("Available = false, want true")
	}
	if status.Error != nil {
		t.Fatalf("Error = %v, want nil", *status.Error)
	}
	if status.ResponseTimeMS == nil {
		t.Fatal("ResponseTimeMS = nil, want non-nil")
	}
	if status.Name != "svc" {
		t.Fatalf("Name = %q, want svc", status.Name)
	}
}

func TestCollectEndpointErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newStubCollector("svc", "/health", srv)
	status := c.collectEndpoint(t.Context(), c.endpoints[0])

	if status.Available {
		t.Fatal("Available = true, want false")
	}
	if status.ResponseTimeMS == nil {
		t.Fatal("ResponseTimeMS = nil, want non-nil on non-2xx")
	}
	if status.Error == nil {
		t.Fatal("Error = nil, want message")
	}
	if want := "status: 503 Service Unavailable"; *status.Error != want {
		t.Fatalf("Error = %q, want %q", *status.Error, want)
	}
}

func TestFetchRuntimeSuccess(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthPayload{Goroutines: 42})
	}))
	defer srv.Close()

	h := newStubHub("svc", "/health", srv)
	stat := h.fetchRuntime(t.Context(), h.endpoints[0])

	if gotPath != "/health" {
		t.Fatalf("requested path = %q, want /health", gotPath)
	}
	if !stat.Available {
		t.Fatal("Available = false, want true")
	}
	if stat.Error != nil {
		t.Fatalf("Error = %v, want nil", *stat.Error)
	}
	if stat.Count != 42 {
		t.Fatalf("Count = %d, want 42", stat.Count)
	}
	if stat.MetricKind != RuntimeMetricGoroutine {
		t.Fatalf("MetricKind = %q, want goroutine", stat.MetricKind)
	}
	if stat.Name != "svc" {
		t.Fatalf("Name = %q, want svc", stat.Name)
	}
}

func TestFetchRuntimeErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	h := newStubHub("svc", "/health", srv)
	stat := h.fetchRuntime(t.Context(), h.endpoints[0])

	if stat.Available {
		t.Fatal("Available = true, want false")
	}
	if stat.Error == nil {
		t.Fatal("Error = nil, want message")
	}
	if want := "status: 500 Internal Server Error"; *stat.Error != want {
		t.Fatalf("Error = %q, want %q", *stat.Error, want)
	}
	if stat.MetricKind != RuntimeMetricGoroutine {
		t.Fatalf("MetricKind = %q, want goroutine", stat.MetricKind)
	}
}

func TestFetchRuntimeComponentFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthPayload{
			Components: map[string]healthComponent{
				"app": {Detail: map[string]any{"goroutines": float64(7)}},
			},
		})
	}))
	defer srv.Close()

	h := newStubHub("svc", "/health", srv)
	stat := h.fetchRuntime(t.Context(), h.endpoints[0])

	if !stat.Available {
		t.Fatal("Available = false, want true")
	}
	if stat.Count != 7 {
		t.Fatalf("Count = %d, want 7 from component fallback", stat.Count)
	}
}

func TestFetchRuntimeInvalidPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	h := newStubHub("svc", "/health", srv)
	stat := h.fetchRuntime(t.Context(), h.endpoints[0])

	if stat.Available {
		t.Fatal("Available = true, want false on invalid payload")
	}
	if stat.Error == nil {
		t.Fatal("Error = nil, want message")
	}
	if got := *stat.Error; !strings.HasPrefix(got, "invalid health payload: ") {
		t.Fatalf("Error = %q, want prefix \"invalid health payload: \"", got)
	}
}
