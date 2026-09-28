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
	}, handler, "test.h3.shutdown", nil)
	if err != nil {
		t.Fatalf("NewRuntimeHTTPServers() error = %v", err)
	}

	packetConn := listenLoopbackUDP(t)
	fixture := &h3ShutdownFixture{servers: servers, addr: packetConn.LocalAddr(), roots: roots, served: make(chan error, 1)}

	go func() { fixture.served <- servers.H3.Serve(packetConn) }()

	return fixture
}

// newClient는 소유한 UDP socket으로 붙는 HTTP/3 client를 만든다. 돌려준 socket을 닫으면 client 프로세스가
// CONNECTION_CLOSE 없이 사라진 것처럼 서버 쪽 연결만 남는다.
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

// 같은 compose stop에서 먼저 종료된 client는 CONNECTION_CLOSE 없이 사라져 서버 연결이 QUIC idle timeout(60s)까지 남는다.
// 종료 시한이 지나도 실행 중인 요청이 없었으면 남은 idle 연결을 닫고 성공해야 한다(v7.0.0 cutover의 worker exit 1).
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

// 실행 중인 요청은 끝날 때까지 기다리고, 그 응답은 client에 전달된다.
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

// 종료 시한까지 끝나지 않은 요청은 실제 종료 실패로 남는다. 시한이 지나면 quic-go가 연결을 닫아 요청 context가 끝난다.
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
