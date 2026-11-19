package httpserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type h3ShutdownFixture struct {
	servers *RuntimeHTTPServers
	addr    net.Addr
	roots   *x509.CertPool
	served  chan error
}

func listenLoopbackUDP(t *testing.T) net.PacketConn {
	t.Helper()

	var config net.ListenConfig

	conn, err := config.ListenPacket(t.Context(), "udp", testLoopbackAddr)
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}

	t.Cleanup(func() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close udp: %v", err)
		}
	})

	return conn
}

func startH3ShutdownFixture(t *testing.T, handler http.Handler) *h3ShutdownFixture {
	t.Helper()

	certFile, keyFile := writeH3LocalhostCertificate(t)

	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load test certificate: %v", err)
	}

	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("parse test certificate: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(leaf)

	servers, err := NewRuntimeHTTPServers(t.Context(), &settings.ServerConfig{
		Port:           30001,
		HTTPTransports: []string{"h3"},
		H3Addr:         testLoopbackAddr,
		H3CertFile:     certFile,
		H3KeyFile:      keyFile,
	}, handler, nil)
	if err != nil {
		t.Fatalf("NewRuntimeHTTPServers() error = %v", err)
	}

	packetConn := listenLoopbackUDP(t)
	fixture := &h3ShutdownFixture{servers: servers, addr: packetConn.LocalAddr(), roots: roots, served: make(chan error, 1)}

	go func() { fixture.served <- servers.H3.Serve(packetConn) }()

	return fixture
}

func (f *h3ShutdownFixture) newClient(t *testing.T) (*http.Client, net.PacketConn) {
	t.Helper()

	clientConn := listenLoopbackUDP(t)
	transport := &http3.Transport{
		TLSClientConfig: &tls.Config{ServerName: "localhost", RootCAs: f.roots, MinVersion: tls.VersionTLS13},
		Dial: func(ctx context.Context, _ string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
			return quic.DialEarly(ctx, clientConn, f.addr, tlsCfg, cfg)
		},
	}

	return &http.Client{Transport: transport, Timeout: 5 * time.Second}, clientConn
}

func h3Get(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://localhost/", http.NoBody)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}

	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("status = %d, want 204", resp.StatusCode)
	}

	return nil
}

func TestRuntimeH3ShutdownSucceedsWhenOnlyVanishedClientConnectionsRemain(t *testing.T) {
	t.Parallel()

	fixture := startH3ShutdownFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	client, clientConn := fixture.newClient(t)

	if err := h3Get(t.Context(), client); err != nil {
		t.Fatalf("GET before shutdown: %v", err)
	}

	if err := clientConn.Close(); err != nil {
		t.Fatalf("close client socket: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	if err := fixture.servers.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() with only an idle connection of a vanished client = %v, want nil", err)
	}

	if err := <-fixture.served; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() after Shutdown = %v, want http.ErrServerClosed", err)
	}
}

func TestRuntimeShutdownStopsMetricsWhileH3WaitsForIdleConnections(t *testing.T) {
	t.Parallel()

	fixture := startH3ShutdownFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	var listenConfig net.ListenConfig

	metricsListener, err := listenConfig.Listen(t.Context(), "tcp", testLoopbackAddr)
	if err != nil {
		t.Fatalf("listen metrics: %v", err)
	}

	fixture.servers.Metrics = NewMetricsServer(t.Context(), testLoopbackAddr, "", nil)

	metricsServed := make(chan error, 1)

	go func() { metricsServed <- fixture.servers.Metrics.Serve(metricsListener) }()

	client, clientConn := fixture.newClient(t)

	if err := h3Get(t.Context(), client); err != nil {
		t.Fatalf("GET before shutdown: %v", err)
	}

	if err := clientConn.Close(); err != nil {
		t.Fatalf("close client socket: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	shutdown := make(chan error, 1)

	go func() { shutdown <- fixture.servers.Shutdown(ctx) }()

	select {
	case err := <-metricsServed:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("metrics Serve() after Shutdown = %v, want http.ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("metrics listener remained open while H3 drained")
	}

	var dialer net.Dialer

	if conn, err := dialer.DialContext(t.Context(), "tcp", metricsListener.Addr().String()); err == nil {
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close late scrape: %v", closeErr)
		}

		t.Fatal("metrics listener accepted a late scrape")
	}

	cancel()

	if err := <-shutdown; err != nil {
		t.Fatalf("Shutdown() with only idle H3 connections = %v, want nil", err)
	}
}

func TestRuntimeH3ShutdownWaitsForInFlightRequest(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	fixture := startH3ShutdownFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	client, _ := fixture.newClient(t)
	response := make(chan error, 1)

	go func() { response <- h3Get(t.Context(), client) }()

	<-entered

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	shutdown := make(chan error, 1)

	go func() { shutdown <- fixture.servers.Shutdown(ctx) }()

	select {
	case err := <-shutdown:
		t.Fatalf("Shutdown() returned %v while a request was still running", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	if err := <-response; err != nil {
		t.Fatalf("in-flight request during shutdown failed: %v", err)
	}

	if err := <-shutdown; err != nil {
		t.Fatalf("Shutdown() after the in-flight request finished = %v, want nil", err)
	}
}

func TestRuntimeH3ShutdownReportsDeadlineWithRequestInFlight(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	fixture := startH3ShutdownFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))

	client, _ := fixture.newClient(t)
	response := make(chan error, 1)

	go func() { response <- h3Get(t.Context(), client) }()

	<-entered

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	if err := fixture.servers.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() with a request still running = %v, want context.DeadlineExceeded", err)
	}

	if err := <-response; err == nil {
		t.Fatal("request cut by the shutdown deadline reported success")
	}
}
