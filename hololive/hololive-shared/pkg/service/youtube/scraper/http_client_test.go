package scraper

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateHTTPClient_DirectHTTP2(t *testing.T) {
	client, err := createHTTPClient(ProxyConfig{})
	require.NoError(t, err)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	assert.True(t, transport.ForceAttemptHTTP2, "direct path should have ForceAttemptHTTP2=true")
}

func TestCreateHTTPClient_ProxyHTTP2(t *testing.T) {
	client, err := createHTTPClient(ProxyConfig{
		Enabled: true,
		URL:     "socks5://127.0.0.1:1080",
	})
	require.NoError(t, err)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "Transport should be *http.Transport")
	assert.False(t, transport.ForceAttemptHTTP2, "proxy path should disable HTTP/2 (single tunnel multiplex is fragile)")
}
