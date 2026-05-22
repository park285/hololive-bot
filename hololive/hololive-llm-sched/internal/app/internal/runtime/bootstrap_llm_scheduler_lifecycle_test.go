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
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

func testRuntimeLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLLMSchedulerRuntimeClose(t *testing.T) {
	t.Run("invokes cleanup", func(t *testing.T) {
		calls := 0
		runtime := &LLMSchedulerRuntime{
			Managed: lifecycle.NewManaged(func() { calls++ }),
		}

		runtime.Close()
		assert.Equal(t, 1, calls)
	})
}

func TestLLMSchedulerRuntimeStartStopSchedulers_NoPanic(t *testing.T) {
	runtime := &LLMSchedulerRuntime{Logger: testRuntimeLogger()}
	ctx := t.Context()

	assert.NotPanics(t, func() { runtime.startSchedulers(ctx) })
	assert.NotPanics(t, runtime.stopSchedulers)
}

func TestLLMSchedulerRuntimeStartHTTPServer_ReportsError(t *testing.T) {
	runtime := &LLMSchedulerRuntime{
		Logger: testRuntimeLogger(),
		httpServer: &http.Server{
			Addr:    "invalid-address",
			Handler: http.NewServeMux(),
		},
	}

	errCh := make(chan error, 1)
	runtime.startHTTPServer(errCh)

	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP server error")
	case <-time.After(2 * time.Second):
		t.Fatal("expected server error from startHTTPServer, got timeout")
	}
}

func TestLLMSchedulerRuntimeShutdown_StopsHTTPServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusNoContent, response.StatusCode)

	runtime := &LLMSchedulerRuntime{
		Logger:     testRuntimeLogger(),
		httpServer: server.Config,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, runtime.Shutdown(ctx))

	_, err = server.Client().Get(server.URL)
	require.Error(t, err)
}

func TestLLMSchedulerRuntimeRun_ReturnsOnServerError(t *testing.T) {
	runtime := &LLMSchedulerRuntime{
		Logger: testRuntimeLogger(),
		httpServer: &http.Server{
			Addr:    "invalid-address",
			Handler: http.NewServeMux(),
		},
	}

	done := make(chan struct{})
	go func() {
		runtime.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not return on server error")
	}
}

func TestBuildLLMSchedulerHTTPServer_WithoutTriggerHandler(t *testing.T) {
	server, err := buildLLMSchedulerHTTPServer(
		context.Background(),
		32077,
		testRuntimeLogger(),
		nil,
		"",
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, server)
	assert.Equal(t, ":32077", server.Addr)

	t.Run("health available", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rr := httptest.NewRecorder()
		server.Handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("trigger route not registered", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, triggercontracts.MemberNewsWeeklyPath, http.NoBody)
		rr := httptest.NewRecorder()
		server.Handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestBuildTriggerRouter_NoTriggerHandler(t *testing.T) {
	router, err := buildTriggerRouter(context.Background(), testRuntimeLogger(), nil, "")
	require.NoError(t, err)
	require.NotNil(t, router)

	req := httptest.NewRequest(http.MethodPost, triggercontracts.MemberNewsWeeklyPath, http.NoBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRR := httptest.NewRecorder()
	router.ServeHTTP(healthRR, healthReq)
	assert.Equal(t, http.StatusOK, healthRR.Code)
	assert.True(t, strings.Contains(healthRR.Body.String(), "status"))
}
