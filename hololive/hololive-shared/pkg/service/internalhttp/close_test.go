package internalhttp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

type closableTransport struct {
	http.RoundTripper

	err error
}

func (t *closableTransport) Close() error {
	return t.err
}

// writePeerCertificate는 127.0.0.1용 자체 서명 인증서를 만들고 client가 신뢰할 PEM 파일 경로와 함께 돌려준다.
func writePeerCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "hololive-internal-peer"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	caFile := filepath.Join(t.TempDir(), "peer.pem")
	if err = os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, caFile
}

// startPeerH3Server는 127.0.0.1 인증서로 HTTP/3 서버를 띄우고, 서버가 받은 QUIC 연결을 돌려주는 channel과
// client가 신뢰할 인증서 파일을 돌려준다.
func startPeerH3Server(t *testing.T) (addr, caFile string, conns <-chan *quic.Conn) {
	t.Helper()

	certificate, caFile := writePeerCertificate(t)

	var listenConfig net.ListenConfig

	packetConn, err := listenConfig.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}

	accepted := make(chan *quic.Conn, 4)
	server := &http3.Server{
		TLSConfig: http3.ConfigureTLSConfig(&tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
		}),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		ConnContext: func(ctx context.Context, conn *quic.Conn) context.Context {
			accepted <- conn

			return ctx
		},
	}

	served := make(chan error, 1)

	go func() { served <- server.Serve(packetConn) }()

	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close peer server: %v", err)
		}

		<-served

		if err := packetConn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close peer udp: %v", err)
		}
	})

	localAddr := packetConn.LocalAddr()
	if localAddr == nil {
		t.Fatal("peer udp socket has no local address")
	}

	return localAddr.String(), caFile, accepted
}

// API가 종료 때 내부 H3 client를 닫지 않으면 peer(alarm-worker 등)는 CONNECTION_CLOSE를 받지 못하고, 그 연결이 QUIC
// idle timeout까지 남아 peer의 graceful shutdown이 종료 시한을 넘긴다. Close는 peer가 원격 application close를 바로
// 받게 해야 한다(idle timeout·stateless reset이 아니라 client가 보낸 CONNECTION_CLOSE).
func TestJSONClientCloseSendsCleanConnectionCloseToPeer(t *testing.T) {
	addr, caFile, conns := startPeerH3Server(t)

	options := sharedh3.ClientOptions{CACertFile: caFile, ServerName: testInternalH3ServerName}

	client, err := NewJSONClient("https://"+addr, "", 5*time.Second, options)
	if err != nil {
		t.Fatalf("NewJSONClient() error = %v", err)
	}

	req, err := client.NewRequest(t.Context(), http.MethodGet, "/")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}

	var peerConn *quic.Conn

	select {
	case peerConn = <-conns:
	case <-time.After(5 * time.Second):
		t.Fatal("peer never accepted the client connection")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-peerConn.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("peer connection still open after Close; the peer would wait for the QUIC idle timeout")
	}

	cause := context.Cause(peerConn.Context())
	if appErr, ok := errors.AsType[*quic.ApplicationError](cause); !ok || !appErr.Remote {
		t.Fatalf("peer close cause = %v, want a CONNECTION_CLOSE sent by the client", cause)
	}
}

func TestCloseClientSurfacesTransportError(t *testing.T) {
	sentinel := errors.New("boom")

	err := CloseClient(&http.Client{Transport: &closableTransport{err: sentinel}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("CloseClient() error = %v, want %v wrapped", err, sentinel)
	}
}

func TestCloseClientIgnoresPlainTransports(t *testing.T) {
	if err := CloseClient(nil); err != nil {
		t.Fatalf("CloseClient(nil) error = %v", err)
	}

	if err := CloseClient(&http.Client{}); err != nil {
		t.Fatalf("CloseClient(default transport) error = %v", err)
	}
}
