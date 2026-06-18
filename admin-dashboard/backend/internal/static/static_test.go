package static

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func testHandler() Handler {
	return Handler{dist: fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"favicon.svg":   {Data: []byte("<svg/>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}}
}

func TestServeIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler().ServeIndex(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	require.Contains(t, rec.Body.String(), "app")
}

func TestServeAssetImmutableCache(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler().ServeAsset(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/assets/app.js", http.NoBody))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))
	require.Contains(t, rec.Header().Get("Content-Type"), "javascript")
}

func TestServeMissingAssetIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	testHandler().ServeAsset(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/assets/missing.js", http.NoBody))

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHasIndex(t *testing.T) {
	require.True(t, testHandler().HasIndex())
	require.False(t, Handler{dist: fstest.MapFS{}}.HasIndex())
}

func TestEmbeddedHandlerServesPlaceholder(t *testing.T) {
	handler := NewHandler()
	rec := httptest.NewRecorder()
	handler.ServeIndex(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", http.NoBody))
	require.Equal(t, http.StatusNotFound, rec.Code)
}
