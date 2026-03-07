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

package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/constants"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProvideAPIServer_ConfigAndHandler(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	server := ProvideAPIServer(":32004", router)
	require.NotNil(t, server)

	assert.Equal(t, ":32004", server.Addr)
	assert.Equal(t, constants.ServerTimeout.ReadHeader, server.ReadHeaderTimeout)
	assert.Equal(t, constants.ServerTimeout.Read, server.ReadTimeout)
	assert.Equal(t, constants.ServerTimeout.Write, server.WriteTimeout)
	assert.Equal(t, constants.ServerTimeout.Idle, server.IdleTimeout)
	assert.Equal(t, constants.ServerTimeout.MaxHeaderBytes, server.MaxHeaderBytes)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "pong", strings.TrimSpace(rr.Body.String()))
}

func TestProvideHealthOnlyRouter_Endpoints(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})

	router, err := ProvideHealthOnlyRouter(context.Background(), newDiscardLogger())
	require.NoError(t, err)
	require.NotNil(t, router)

	assert.Equal(t, gin.ReleaseMode, gin.Mode())
	assert.Equal(t, gin.PlatformCloudflare, router.TrustedPlatform)

	t.Run("health", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &payload))
		assert.Equal(t, "ok", payload["status"])
	})

	t.Run("metrics", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.NotEmpty(t, rr.Body.String())
		assert.Contains(t, rr.Header().Get("Content-Type"), "text/plain")
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestBuildLLMSchedulerHTTPServer_HealthAndTrigger(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})

	logger := newDiscardLogger()
	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

	server, err := buildLLMSchedulerHTTPServer(
		context.Background(),
		32005,
		logger,
		triggerHandler,
		"",
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, server)

	assert.Equal(t, ":32005", server.Addr)

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRR := httptest.NewRecorder()
	server.Handler.ServeHTTP(healthRR, healthReq)
	assert.Equal(t, http.StatusOK, healthRR.Code)

	triggerReq := httptest.NewRequest(http.MethodPost, triggercontracts.MemberNewsWeeklyPath, http.NoBody)
	triggerRR := httptest.NewRecorder()
	server.Handler.ServeHTTP(triggerRR, triggerReq)
	assert.Equal(t, http.StatusServiceUnavailable, triggerRR.Code)
}

func TestBuildLLMSchedulerHTTPServer_WithAPIKey(t *testing.T) {
	prevMode := gin.Mode()
	t.Cleanup(func() {
		gin.SetMode(prevMode)
	})

	logger := newDiscardLogger()
	triggerHandler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)

	server, err := buildLLMSchedulerHTTPServer(
		context.Background(),
		32006,
		logger,
		triggerHandler,
		"test-key",
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, server)

	withoutKeyReq := httptest.NewRequest(http.MethodPost, triggercontracts.MemberNewsWeeklyPath, http.NoBody)
	withoutKeyRR := httptest.NewRecorder()
	server.Handler.ServeHTTP(withoutKeyRR, withoutKeyReq)
	assert.Equal(t, http.StatusUnauthorized, withoutKeyRR.Code)

	withKeyReq := httptest.NewRequest(http.MethodPost, triggercontracts.MemberNewsWeeklyPath, http.NoBody)
	withKeyReq.Header.Set(middleware.APIKeyHeader, "test-key")
	withKeyRR := httptest.NewRecorder()
	server.Handler.ServeHTTP(withKeyRR, withKeyReq)
	assert.Equal(t, http.StatusServiceUnavailable, withKeyRR.Code)
}
