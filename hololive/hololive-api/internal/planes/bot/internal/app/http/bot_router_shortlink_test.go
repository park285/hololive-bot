package apphttp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBotRouteRegistrarIncludesPublicShortLink(t *testing.T) {
	t.Parallel()

	router := gin.New()
	err := botRouteRegistrar("", nil, nil, nil, slog.New(slog.DiscardHandler))(router)
	require.NoError(t, err)

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/l/dQw4w9WgXcQ", http.NoBody)
	request.Header.Set("User-Agent", "facebookexternalhit/1.1; kakaotalk-scrap/1.0")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusForbidden, response.Code)
}
