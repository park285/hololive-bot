package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildShortLinkServerDisabledWithoutAddress(t *testing.T) {
	t.Parallel()

	assert.Nil(t, BuildShortLinkServer(" \t"))
}

func TestBuildShortLinkServerServesOnlyShortLinks(t *testing.T) {
	t.Parallel()

	server := BuildShortLinkServer(" 127.0.0.1:30101 ")
	require.NotNil(t, server)
	assert.Equal(t, "127.0.0.1:30101", server.Addr)

	shortLink := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/l/dQw4w9WgXcQ", http.NoBody)
	shortLinkResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(shortLinkResponse, shortLink)
	assert.Equal(t, http.StatusFound, shortLinkResponse.Code)

	health := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
	healthResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(healthResponse, health)
	assert.Equal(t, http.StatusNotFound, healthResponse.Code)
}
