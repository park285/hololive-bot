package docker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestListContainersCoalescesConcurrentCacheMisses(t *testing.T) {
	var requests atomic.Int32
	release := make(chan struct{})
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"Id":"1","Names":["/hololive-api"],"Image":"img","Status":"Up","State":"running","Created":1}]`)
	}))

	const callers = 16
	start := make(chan struct{})
	results := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			_, err := client.ListContainers(context.Background())
			results <- err
		}()
	}
	ready.Wait()
	close(start)

	deadline := time.Now().Add(time.Second)
	for requests.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("in-flight docker list requests = %d, want 1", got)
	}
	time.Sleep(30 * time.Millisecond)
	if got := requests.Load(); got != 1 {
		t.Fatalf("concurrent cache miss stampede made %d upstream requests, want 1", got)
	}
	close(release)

	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("ListContainers() error = %v", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("total upstream requests = %d, want 1", got)
	}
}

func TestListContainersCacheReturnsDeepCopies(t *testing.T) {
	const body = `[{"Id":"1","Names":["/hololive-api"],"Image":"img","Status":"Up (healthy)","State":"running","Created":1,"Ports":[{"PrivatePort":30001,"PublicPort":40001,"Type":"tcp"}]}]`
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))

	first, err := client.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("prime cache: %v", err)
	}
	if len(first) != 1 || first[0].Health == nil || len(first[0].Ports) != 1 || first[0].Ports[0].PublicPort == nil {
		t.Fatalf("unexpected first container: %+v", first)
	}
	*first[0].Health = "leader-mutated"
	first[0].Ports[0].PortType = "leader-udp"
	*first[0].Ports[0].PublicPort = 65534

	cached, err := client.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if len(cached) != 1 || cached[0].Health == nil || len(cached[0].Ports) != 1 || cached[0].Ports[0].PublicPort == nil {
		t.Fatalf("unexpected cached container: %+v", cached)
	}
	*cached[0].Health = "mutated"
	cached[0].Ports[0].PortType = "udp"
	*cached[0].Ports[0].PublicPort = 65535

	again, err := client.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("read cache again: %v", err)
	}
	if got := *again[0].Health; got != "healthy" {
		t.Fatalf("cached health = %q, want healthy", got)
	}
	if got := again[0].Ports[0].PortType; got != "tcp" {
		t.Fatalf("cached port type = %q, want tcp", got)
	}
	if got := *again[0].Ports[0].PublicPort; got != 40001 {
		t.Fatalf("cached public port = %d, want 40001", got)
	}
}

func TestListContainersWaiterHonorsCancellation(t *testing.T) {
	release := make(chan struct{})
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = io.WriteString(w, `[]`)
	}))

	leaderDone := make(chan error, 1)
	go func() {
		_, err := client.ListContainers(context.Background())
		leaderDone <- err
	}()

	deadline := time.Now().Add(time.Second)
	for {
		client.refreshMu.Lock()
		inFlight := client.refresh != nil
		client.refreshMu.Unlock()
		if inFlight {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("leader refresh did not start")
		}
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ListContainers(ctx); err == nil {
		t.Fatal("canceled cache waiter error = nil")
	} else if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cache waiter error = %v, want context.Canceled cause", err)
	}
	close(release)
	if err := <-leaderDone; err != nil {
		t.Fatalf("leader ListContainers() error = %v", err)
	}
}

func TestListContainersRejectsOversizedResponse(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", maxDockerListResponseBytes+1))
	}))
	if _, err := client.ListContainers(context.Background()); err == nil {
		t.Fatal("oversized docker response error = nil")
	}
}

func TestDockerActionDrainsErrorBodyForKeepAliveReuse(t *testing.T) {
	var newConnections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "temporary docker proxy failure")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	host := strings.TrimPrefix(server.URL, "http://")
	client, err := NewClient("tcp://" + host)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	for range 2 {
		if err := client.RestartContainer(context.Background(), "hololive-api"); err == nil {
			t.Fatal("RestartContainer() error = nil, want upstream failure")
		}
	}
	client.http.CloseIdleConnections()
	if got := newConnections.Load(); got != 1 {
		t.Fatalf("new connections = %d, want 1 after draining sequential action responses", got)
	}
}
