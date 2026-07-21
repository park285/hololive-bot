package docker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/admin-dashboard/internal/httpx"
)

func TestContainerActionFencesOlderListRefreshFromCache(t *testing.T) {
	var listRequests atomic.Int32
	firstListStarted := make(chan struct{})
	releaseFirstList := make(chan struct{})
	var startOnce sync.Once
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseFirstList) }) })

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/json":
			requestNumber := listRequests.Add(1)
			name := "hololive-after-action"
			if requestNumber == 1 {
				startOnce.Do(func() { close(firstListStarted) })
				<-releaseFirstList
				name = "hololive-before-action"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `[{"Id":"%d","Names":["/%s"],"Image":"img","Status":"Up","State":"running","Created":1}]`, requestNumber, name)
		case r.Method == http.MethodPost && r.URL.Path == "/containers/hololive-api/restart":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))

	firstResult := make(chan []Container, 1)
	firstErr := make(chan error, 1)
	go func() {
		containers, err := client.ListContainers(context.Background())
		firstResult <- containers
		firstErr <- err
	}()

	select {
	case <-firstListStarted:
	case <-time.After(time.Second):
		t.Fatal("first docker list refresh did not start")
	}

	if err := client.RestartContainer(context.Background(), "hololive-api"); err != nil {
		t.Fatalf("RestartContainer() error = %v", err)
	}
	releaseOnce.Do(func() { close(releaseFirstList) })

	if err := <-firstErr; err != nil {
		t.Fatalf("first ListContainers() error = %v", err)
	}
	first := <-firstResult
	if len(first) != 1 || first[0].Name != "hololive-before-action" {
		t.Fatalf("first containers = %+v", first)
	}

	second, err := client.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("second ListContainers() error = %v", err)
	}
	if got := listRequests.Load(); got != 2 {
		t.Fatalf("docker list requests = %d, want 2 after action invalidated the in-flight refresh", got)
	}
	if len(second) != 1 || second[0].Name != "hololive-after-action" {
		t.Fatalf("second containers = %+v, want post-action refresh", second)
	}
}

func TestDefaultContainerPolicyMatchesComposeOwnership(t *testing.T) {
	client, err := NewClient("tcp://127.0.0.1:2375")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	tests := map[string]struct {
		managed     bool
		stopBlocked bool
	}{
		"hololive-api":                {managed: true},
		"hololive-youtube-producer-a": {managed: true},
		"holo-postgres":               {managed: true, stopBlocked: true},
		"valkey-cache":                {managed: true, stopBlocked: true},
		"admin-dashboard":             {managed: true, stopBlocked: true},
		"deunhealth":                  {managed: true, stopBlocked: true},
		"hololive-db-migrate":         {},
		"hololive-api-init":           {},
		"hololiveevil":                {},
		"administrator":               {},
		"postgresql":                  {},
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			if got := client.IsManaged(name); got != want.managed {
				t.Fatalf("IsManaged(%q) = %v, want %v", name, got, want.managed)
			}
			if got := client.stopBlocked(name); got != want.stopBlocked {
				t.Fatalf("stopBlocked(%q) = %v, want %v", name, got, want.stopBlocked)
			}
		})
	}
}

func TestMigrationContainerActionsFailClosedBeforeDockerIO(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	err := client.RestartContainer(context.Background(), "hololive-db-migrate")
	if err == nil {
		t.Fatal("RestartContainer() error = nil for migration container")
	}
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Status != http.StatusNotFound {
		t.Fatalf("RestartContainer() error = %v, want fail-closed 404", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Docker requests = %d, want 0 for excluded migration container", got)
	}
}

func TestDockerListPreservesCancellationCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &Client{
		baseURL:         "http://docker",
		http:            &http.Client{Transport: dockerRoundTripFunc(func(req *http.Request) (*http.Response, error) { return nil, req.Context().Err() })},
		managedPrefixes: []string{"hololive"},
		listTimeout:     time.Second,
		cacheTTL:        time.Second,
	}

	_, err := client.ListContainers(ctx)
	if err == nil {
		t.Fatal("ListContainers() error = nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListContainers() error = %v, want context.Canceled cause", err)
	}
}

func TestUnsupportedDockerHostErrorDoesNotEchoInput(t *testing.T) {
	const host = "ssh://operator:secret@docker.example"
	_, _, err := dockerHTTPTransport(host)
	if err == nil {
		t.Fatal("dockerHTTPTransport() error = nil")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), host) {
		t.Fatalf("dockerHTTPTransport() error leaks host input: %v", err)
	}
}

type dockerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f dockerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
