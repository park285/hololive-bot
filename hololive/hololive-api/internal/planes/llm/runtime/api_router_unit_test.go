// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package runtime

import (
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestProvideAPIServer_ConfigAndHandler(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	server := sharedserver.NewHTTPServer(":32004", router, "")
	require.NotNil(t, server)

	assert.Equal(t, ":32004", server.Addr)
	// httpserver.NewHTTPServer가 공개 런타임 서버에 거는 연결 제한입니다.
	assert.Equal(t, 5*time.Second, server.ReadHeaderTimeout)
	assert.Equal(t, 15*time.Second, server.ReadTimeout)
	assert.Equal(t, 60*time.Second, server.WriteTimeout)
	assert.Equal(t, 60*time.Second, server.IdleTimeout)
	assert.Equal(t, 1<<20, server.MaxHeaderBytes)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ping", http.NoBody)
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "pong", strings.TrimSpace(rr.Body.String()))
}

func TestBuildLLMSchedulerRouter_Endpoints(t *testing.T) {
	prevMode := gin.Mode()

	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})

	router, err := buildLLMSchedulerRouter(t.Context(), newDiscardLogger(), "test-key", nil)
	require.NoError(t, err)
	require.NotNil(t, router)

	assert.Equal(t, gin.ReleaseMode, gin.Mode())
	assert.Equal(t, gin.PlatformCloudflare, router.TrustedPlatform)

	t.Run("health", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/health", http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var payload map[string]any

		require.NoError(t, jsonv2.Unmarshal(rr.Body.Bytes(), &payload))
		assert.Equal(t, "ok", payload["status"])
	})

	t.Run("metrics require api key", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)

		reqWithKey := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody)
		reqWithKey.Header.Set(common.APIKeyHeader, "test-key")

		rrWithKey := httptest.NewRecorder()
		router.ServeHTTP(rrWithKey, reqWithKey)
		assert.Equal(t, http.StatusOK, rrWithKey.Code)
		assert.NotEmpty(t, rrWithKey.Body.String())
		assert.Contains(t, rrWithKey.Header().Get("Content-Type"), "text/plain")
	})

	t.Run("ready", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var payload map[string]any

		require.NoError(t, jsonv2.Unmarshal(rr.Body.Bytes(), &payload))
		assert.Equal(t, "ok", payload["status"])
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/unknown", http.NoBody)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}
