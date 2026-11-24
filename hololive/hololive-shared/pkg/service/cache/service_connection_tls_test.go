package cache

import (
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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCacheTLSConfigRequiresTLS13(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cache"}, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))

	client, err := cacheTLSConfig(caFile)
	require.NoError(t, err)

	for _, tc := range []struct {
		maxVersion uint16
		accepted   bool
	}{{tls.VersionTLS12, false}, {tls.VersionTLS13, true}} {
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)

		served := make(chan error, 1)

		go func() {
			raw, acceptErr := listener.Accept()
			if acceptErr != nil {
				served <- acceptErr

				return
			}

			server := tls.Server(raw, &tls.Config{
				Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12, MaxVersion: tc.maxVersion,
			})
			served <- errors.Join(server.HandshakeContext(t.Context()), server.Close())
		}()

		conn, dialErr := (&tls.Dialer{Config: client.Clone()}).DialContext(t.Context(), "tcp", listener.Addr().String())

		if tc.accepted {
			require.NoError(t, dialErr)

			tlsConn, ok := conn.(*tls.Conn)
			require.True(t, ok)
			require.Equal(t, uint16(tls.VersionTLS13), tlsConn.ConnectionState().Version)
			require.NoError(t, conn.Close())
			require.NoError(t, <-served)
		} else {
			require.Error(t, dialErr)
			require.Error(t, <-served)
		}

		require.NoError(t, listener.Close())
	}
}
