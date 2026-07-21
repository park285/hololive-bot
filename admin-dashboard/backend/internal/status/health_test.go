package status

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func newStubHub(srv *httptest.Server) *Hub {
	const name = "svc"
	const healthPath = "/health"
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
		if err := json.NewEncoder(w).Encode(healthPayload{Goroutines: 42}); err != nil {
			t.Errorf("encode health response: %v", err)
		}
	}))
	defer srv.Close()

	h := newStubHub(srv)
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

	h := newStubHub(srv)
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
		if err := json.NewEncoder(w).Encode(healthPayload{
			Components: map[string]healthComponent{
				"app": {Detail: map[string]any{"goroutines": float64(7)}},
			},
		}); err != nil {
			t.Errorf("encode component health response: %v", err)
		}
	}))
	defer srv.Close()

	h := newStubHub(srv)
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
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write invalid health response: %v", err)
		}
	}))
	defer srv.Close()

	h := newStubHub(srv)
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

func TestDoHealthGETBoundsAndReplaysSuccessfulBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","goroutines":12}`)
	}))
	defer server.Close()

	result := doHealthGET(t.Context(), endpointClient{client: server.Client()}, ServiceEndpoint{
		Name:       "service",
		URL:        server.URL,
		HealthPath: "/health",
	})
	if result.errMsg != "" {
		t.Fatalf("doHealthGET() error = %q", result.errMsg)
	}
	body, err := io.ReadAll(result.resp.Body)
	if err != nil {
		t.Fatalf("read replayed body: %v", err)
	}
	if got := string(body); got != `{"status":"ok","goroutines":12}` {
		t.Fatalf("replayed body = %q", got)
	}
	if err := result.resp.Body.Close(); err != nil {
		t.Fatalf("close replayed body: %v", err)
	}
}

func TestDoHealthGETRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", int(maxHealthResponseBodyBytes)+1))
	}))
	defer server.Close()

	result := doHealthGET(t.Context(), endpointClient{client: server.Client()}, ServiceEndpoint{
		Name:       "service",
		URL:        server.URL,
		HealthPath: "/health",
	})
	if !strings.Contains(result.errMsg, "exceeds limit") {
		t.Fatalf("error = %q, want oversized-body error", result.errMsg)
	}
	if !result.measured {
		t.Fatal("oversized response should retain measured latency")
	}
}

func TestDoHealthGETDrainsErrorBodyForKeepAliveReuse(t *testing.T) {
	var newConnections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "temporarily unavailable")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	client := server.Client()
	endpoint := ServiceEndpoint{Name: "service", URL: server.URL, HealthPath: "/health"}
	for range 2 {
		result := doHealthGET(t.Context(), endpointClient{client: client}, endpoint)
		if !strings.Contains(result.errMsg, "503") {
			t.Fatalf("error = %q, want 503 status", result.errMsg)
		}
	}
	client.CloseIdleConnections()
	if got := newConnections.Load(); got != 1 {
		t.Fatalf("new connections = %d, want 1 after draining sequential error responses", got)
	}
}
