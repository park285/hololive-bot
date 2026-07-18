package docker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kapu/admin-dashboard/internal/httpx"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	client, err := NewClient("tcp://" + host)
	if err != nil {
		t.Fatalf("NewClient(tcp://%s) error = %v", host, err)
	}
	return client
}

func TestListContainersFiltersToManagedAndSorts(t *testing.T) {
	const body = `[
		{"Id":"1","Names":["/zeta"],"Image":"img","Status":"Up (healthy)","State":"running","Created":1},
		{"Id":"2","Names":["/hololive-admin-api"],"Image":"img","Status":"Up","State":"running","Created":2},
		{"Id":"3","Names":["/random-thing"],"Image":"img","Status":"Up","State":"running","Created":3},
		{"Id":"4","Names":["/admin-dashboard"],"Image":"img","Status":"Up","State":"running","Created":4},
		{"Id":"5","Names":["/hololive-alarm-init"],"Image":"img","Status":"Up","State":"running","Created":5}
	]`
	var gotPath, gotMethod string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write docker response: %v", err)
		}
	}))

	containers, err := client.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("request method = %q, want GET", gotMethod)
	}
	if gotPath != "/containers/json" {
		t.Fatalf("request path = %q, want /containers/json", gotPath)
	}
	names := make([]string, len(containers))
	for i, c := range containers {
		names[i] = c.Name
	}
	if len(names) != 2 || names[0] != "admin-dashboard" || names[1] != "hololive-admin-api" {
		t.Fatalf("managed+sorted names = %v, want [admin-dashboard hololive-admin-api]", names)
	}
}

func TestListContainersMaps5xxToInternal(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_, err := client.ListContainers(context.Background())
	if err == nil {
		t.Fatal("ListContainers error = nil, want error for upstream 500")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error type = %T, want httpx.AppError", err)
	}
	if appErr.Status != http.StatusInternalServerError {
		t.Fatalf("mapped status = %d, want 500", appErr.Status)
	}
}

func TestRestartContainerPinsMethodAndURL(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))

	if err := client.RestartContainer(context.Background(), "hololive-admin-api"); err != nil {
		t.Fatalf("RestartContainer error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("action method = %q, want POST", gotMethod)
	}
	if gotPath != "/containers/hololive-admin-api/restart" {
		t.Fatalf("action path = %q, want /containers/hololive-admin-api/restart", gotPath)
	}
	if gotQuery != "t=30" {
		t.Fatalf("action query = %q, want t=30", gotQuery)
	}
}

func TestRestartContainerRejectsUnmanagedBeforeRequest(t *testing.T) {
	called := false
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	err := client.RestartContainer(context.Background(), "random-thing")
	if err == nil {
		t.Fatal("RestartContainer on unmanaged name error = nil, want 404 gate")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusNotFound {
		t.Fatalf("error = %v (%T), want httpx.AppError 404", err, err)
	}
	if called {
		t.Fatal("unmanaged action reached the docker server; gate must fail-closed before the request")
	}
}

func TestStopContainerRejectsInfraBeforeRequest(t *testing.T) {
	called := false
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	err := client.StopContainer(context.Background(), "valkey-cache")
	if err == nil {
		t.Fatal("StopContainer on infra name error = nil, want 403 gate")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusForbidden {
		t.Fatalf("error = %v (%T), want httpx.AppError 403", err, err)
	}
	if called {
		t.Fatal("infra stop reached the docker server; gate must fail-closed before the request")
	}
}

func TestRestartInfraAllowed(t *testing.T) {
	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))

	if err := client.RestartContainer(context.Background(), "valkey-cache"); err != nil {
		t.Fatalf("RestartContainer on infra name error = %v, want success", err)
	}
	if gotPath != "/containers/valkey-cache/restart" {
		t.Fatalf("action path = %q, want /containers/valkey-cache/restart", gotPath)
	}
}

func TestStopAppContainerAllowed(t *testing.T) {
	var gotPath, gotQuery string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	}))

	if err := client.StopContainer(context.Background(), "hololive-admin-api"); err != nil {
		t.Fatalf("StopContainer on app container error = %v, want success", err)
	}
	if gotPath != "/containers/hololive-admin-api/stop" || gotQuery != "t=30" {
		t.Fatalf("action = %q?%q, want /containers/hololive-admin-api/stop?t=30", gotPath, gotQuery)
	}
}

func TestListContainersSetsManagedAndStopBlocked(t *testing.T) {
	const body = `[
		{"Id":"1","Names":["/hololive-admin-api"],"Image":"img","Status":"Up","State":"running","Created":1},
		{"Id":"2","Names":["/valkey-cache"],"Image":"img","Status":"Up","State":"running","Created":2}
	]`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write docker response: %v", err)
		}
	}))

	containers, err := client.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers error = %v", err)
	}
	byName := make(map[string]Container, len(containers))
	for _, c := range containers {
		byName[c.Name] = c
	}
	app, ok := byName["hololive-admin-api"]
	if !ok || !app.Managed || app.StopBlocked {
		t.Fatalf("hololive-admin-api = %+v, want Managed=true StopBlocked=false", app)
	}
	infra, ok := byName["valkey-cache"]
	if !ok || !infra.Managed || !infra.StopBlocked {
		t.Fatalf("valkey-cache = %+v, want Managed=true StopBlocked=true", infra)
	}
}

func TestActionBudgetExceedsListBudget(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`[]`)); err != nil {
			t.Errorf("write docker response: %v", err)
		}
	}))
	client.listTimeout = 80 * time.Millisecond
	client.actionTimeout = 2 * time.Second

	if _, err := client.ListContainers(context.Background()); err == nil {
		t.Fatal("ListContainers error = nil, want timeout with short list budget")
	}
	if err := client.RestartContainer(context.Background(), "hololive-admin-api"); err != nil {
		t.Fatalf("RestartContainer error = %v, want success within action budget", err)
	}
}

func TestActionTimesOutOnHungDocker(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	client.actionTimeout = 50 * time.Millisecond

	err := client.RestartContainer(context.Background(), "hololive-admin-api")
	if err == nil {
		t.Fatal("RestartContainer error = nil, want timeout on hung docker")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("error = %v (%T), want httpx.AppError 503", err, err)
	}
}

func TestRestartContainerMaps404FromDocker(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	err := client.RestartContainer(context.Background(), "hololive-admin-api")
	if err == nil {
		t.Fatal("RestartContainer error = nil, want 404 mapped from docker")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusNotFound {
		t.Fatalf("error = %v (%T), want httpx.AppError 404", err, err)
	}
}

func TestRestartContainerMaps5xxToInternal(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	err := client.RestartContainer(context.Background(), "hololive-admin-api")
	if err == nil {
		t.Fatal("RestartContainer error = nil, want error for docker 5xx")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusInternalServerError {
		t.Fatalf("error = %v (%T), want httpx.AppError 500", err, err)
	}
}
