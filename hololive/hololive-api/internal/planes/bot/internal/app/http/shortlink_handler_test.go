package apphttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestYouTubeShortLinkRedirectsRegularClients(t *testing.T) {
	t.Parallel()

	response := serveShortLinkRequest(t, http.MethodGet, "/l/dQw4w9WgXcQ", "KAKAOTALK 26.1.0")

	assert.Equal(t, http.StatusFound, response.Code)
	assert.Equal(t, domain.YouTubeWatchURL("dQw4w9WgXcQ"), response.Header().Get("Location"))
	assert.Equal(t, "no-store, max-age=0", response.Header().Get("Cache-Control"))
	assert.Equal(t, "User-Agent", response.Header().Get("Vary"))
	assert.Contains(t, response.Header().Get("X-Robots-Tag"), "noimageindex")
}

func TestYouTubeShortLinkBlocksKakaoTalkScraper(t *testing.T) {
	t.Parallel()

	response := serveShortLinkRequest(
		t,
		http.MethodGet,
		"/l/dQw4w9WgXcQ",
		"facebookexternalhit/1.1; kakaotalk-scrap/1.0",
	)

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Empty(t, response.Header().Get("Location"))
	assert.Empty(t, response.Body.String())
}

func TestYouTubeShortLinkBlocksScraperOnHead(t *testing.T) {
	t.Parallel()

	response := serveShortLinkRequest(t, http.MethodHead, "/l/dQw4w9WgXcQ", "KakaoTalk-Scrap/1.0")

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Empty(t, response.Header().Get("Location"))
}

func TestYouTubeShortLinkRejectsInvalidVideoID(t *testing.T) {
	t.Parallel()

	response := serveShortLinkRequest(t, http.MethodGet, "/l/not-valid", "Mozilla/5.0")

	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.Empty(t, response.Header().Get("Location"))
}

func TestShortLinkHandlerDoesNotExposeBotRoutes(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
	response := httptest.NewRecorder()
	ProvideShortLinkHandler().ServeHTTP(response, request)

	assert.Equal(t, http.StatusNotFound, response.Code)
}

func serveShortLinkRequest(t *testing.T, method, path, userAgent string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), method, path, http.NoBody)
	request.Header.Set("User-Agent", userAgent)

	response := httptest.NewRecorder()
	ProvideShortLinkHandler().ServeHTTP(response, request)

	return response
}
