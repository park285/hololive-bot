package botruntime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

func TestBotRuntimeCloseWaitsForRealHTTPHandlerBeforeFreeingResources(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })

	defer finish()

	var cleanups atomic.Int64

	runtime := &BotRuntime{cleanup: func() error {
		cleanups.Add(1)

		return nil
	}}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	runtime.ShortLinkServer = server.Config
	runtime.prepareHTTPHandlers()
	server.Start()
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	responseDone := make(chan error, 1)

	go func() {
		responseDone <- performRuntimeTestRequest(server.Client(), req)
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not enter the handler")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()

	if err := runtime.CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext with live request=%v want deadline", err)
	}

	if got := cleanups.Load(); got != 0 {
		t.Fatalf("cleanup with live request=%d want=0", got)
	}

	finish()

	if err := <-responseDone; err != nil {
		t.Fatal(err)
	}

	if err := runtime.CloseContext(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup after request exit=%d want=1", got)
	}
}

func performRuntimeTestRequest(client *http.Client, req *http.Request) error {
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send test request: %w", err)
	}

	if response == nil {
		return errors.New("test request returned no response")
	}

	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return errors.Join(fmt.Errorf("test response status=%d want=204", response.StatusCode), closeErr)
	}

	return closeErr
}

func TestBotRuntimeRejectsNewRealHTTPRequestsAfterAdmissionStops(t *testing.T) {
	var handlers atomic.Int64

	runtime := &BotRuntime{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlers.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	runtime.ShortLinkServer = server.Config
	runtime.prepareHTTPHandlers()
	server.Start()
	t.Cleanup(server.Close)
	t.Cleanup(runtime.Close)
	runtime.stopHTTPRequestAdmission()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if response == nil {
		t.Fatal("request returned no response")
	}

	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != http.StatusServiceUnavailable || handlers.Load() != 0 {
		t.Fatalf("closed admission status=%d handler calls=%d want=503/0", response.StatusCode, handlers.Load())
	}
}

func TestBotRuntimeClosesResourcesAfterIdleOnlyH3ShutdownError(t *testing.T) {
	var cleanups atomic.Int64

	runtime := &BotRuntime{cleanup: func() error {
		cleanups.Add(1)

		return nil
	}}
	serverAddr, client := newRuntimeH3TestPeer(t, runtime)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+serverAddr, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	if err := performRuntimeTestRequest(client.http, req); err != nil {
		t.Fatal(err)
	}

	// CONNECTION_CLOSE 없이 peer UDP socket만 없어지는 기존 재현을 유지한다.
	if err := client.socket.Close(); err != nil {
		t.Fatal(err)
	}

	drainCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	if err := runtime.Shutdown(drainCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("idle vanished peer shutdown=%v want preserved deadline", err)
	}

	closeCtx, cancelClose := context.WithTimeout(t.Context(), time.Second)
	defer cancelClose()

	if err := runtime.CloseContext(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext error=%v want preserved shutdown error", err)
	}

	if got := cleanups.Load(); got != 1 {
		t.Fatalf("cleanup with zero live HTTP users=%d want=1", got)
	}
}

type runtimeH3TestPeer struct {
	http   *http.Client
	socket net.PacketConn
}

func newRuntimeH3TestPeer(t *testing.T, runtime *BotRuntime) (string, runtimeH3TestPeer) {
	t.Helper()

	serverAddr, roots := newRuntimeH3TestServer(t, runtime)

	clientSocket, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	quicTransport := &quic.Transport{Conn: clientSocket}
	transport := &http3.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots},
		Dial: func(ctx context.Context, addr string, tlsConfig *tls.Config, config *quic.Config) (*quic.Conn, error) {
			peerAddr, err := netip.ParseAddrPort(addr)
			if err != nil {
				return nil, fmt.Errorf("parse test peer address: %w", err)
			}

			return quicTransport.DialEarly(ctx, net.UDPAddrFromAddrPort(peerAddr), tlsConfig, config)
		},
	}

	t.Cleanup(func() {
		if err := transport.Close(); err != nil {
			t.Errorf("close H3 test client: %v", err)
		}

		if err := quicTransport.Close(); err != nil {
			t.Errorf("close test QUIC transport: %v", err)
		}
	})

	return serverAddr, runtimeH3TestPeer{http: &http.Client{Transport: transport, Timeout: time.Second}, socket: clientSocket}
}

func newRuntimeH3TestServer(t *testing.T, runtime *BotRuntime) (string, *x509.CertPool) {
	t.Helper()

	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(fixture.Close)

	roots := x509.NewCertPool()
	roots.AddCert(fixture.Certificate())

	serverSocket, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	runtime.H3Server = &http3.Server{
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: fixture.TLS.Certificates},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	runtime.prepareHTTPHandlers()

	go func() {
		if serveErr := runtime.H3Server.Serve(serverSocket); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Errorf("serve H3 test peer: %v", serveErr)
		}
	}()

	t.Cleanup(func() {
		if closeErr := runtime.H3Server.Close(); closeErr != nil {
			t.Errorf("close H3 test server: %v", closeErr)
		}

		if closeErr := serverSocket.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close H3 test server socket: %v", closeErr)
		}
	})

	addr := serverSocket.LocalAddr()
	if addr == nil {
		t.Fatal("H3 test server socket has no local address")
	}

	return addr.String(), roots
}
