package scraping

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextAwareStubDialer struct {
	conn  net.Conn
	err   error
	calls int
}

func (d *contextAwareStubDialer) Dial(_, _ string) (net.Conn, error) {
	return nil, errors.New("unexpected Dial call")
}

func (d *contextAwareStubDialer) DialContext(_ context.Context, _, _ string) (net.Conn, error) {
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	return d.conn, nil
}

func TestNewScraperTransport_UsesSharedConnectionProfile(t *testing.T) {
	directTransport := newScraperTransport(true)
	proxyTransport := newScraperTransport(false)

	require.NotNil(t, directTransport)
	require.NotNil(t, proxyTransport)
	assert.True(t, directTransport.ForceAttemptHTTP2)
	assert.False(t, proxyTransport.ForceAttemptHTTP2)
	assert.Equal(t, directTransport.MaxIdleConns, proxyTransport.MaxIdleConns)
	assert.Equal(t, directTransport.MaxIdleConnsPerHost, proxyTransport.MaxIdleConnsPerHost)
	assert.Equal(t, directTransport.IdleConnTimeout, proxyTransport.IdleConnTimeout)
	assert.Equal(t, directTransport.TLSHandshakeTimeout, proxyTransport.TLSHandshakeTimeout)
	assert.Equal(t, directTransport.ResponseHeaderTimeout, proxyTransport.ResponseHeaderTimeout)
	assert.Equal(t, directTransport.ExpectContinueTimeout, proxyTransport.ExpectContinueTimeout)
}

func TestNewSOCKS5DialContext_PrefersContextDialer(t *testing.T) {
	clientSide, peerSide := net.Pipe()
	t.Cleanup(func() {
		mustClose(t, clientSide)
		mustClose(t, peerSide)
	})

	dialer := &contextAwareStubDialer{conn: clientSide}
	dialContext := newSOCKS5DialContext(dialer)

	conn, err := dialContext(context.Background(), "tcp", "example.com:443")
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.Equal(t, 1, dialer.calls)
}
