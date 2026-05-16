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

package botruntime

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
)

func TestProvideTriggerHandler_ReturnsUsableHandler(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	handler := sharedserver.NewTriggerHandler(nil, nil, nil, logger)
	require.NotNil(t, handler)

	router := gin.New()
	handler.RegisterInternalRoutesWithAuth(router.Group(""), "")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, triggercontracts.MajorEventWeeklyPath, http.NoBody)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(t, http.StatusServiceUnavailable, res.Code)
}

func TestBuildBotWebhookHandler_ConstructsAndHandlesMethodGuard(t *testing.T) {
	t.Setenv("IRIS_WEBHOOK_TOKEN", "test-token")

	cfg := &config.Config{
		Iris: config.IrisConfig{
			WebhookToken: "test-token",
		},
		Webhook: config.WebhookConfig{
			WorkerCount:    1,
			QueueSize:      8,
			EnqueueTimeout: 10 * time.Millisecond,
			HandlerTimeout: 50 * time.Millisecond,
		},
	}
	deps := botWebhookRuntimeDependencies{
		Cache: &cachemocks.Client{
			GetClientFunc: func() valkey.Client { return nil },
		},
	}

	handler, err := appbootstrap.BuildBotWebhookHandler(cfg, stubWebhookMessageHandler{}, deps, nil)
	require.NoError(t, err)
	require.NotNil(t, handler)
	t.Cleanup(func() {
		_ = handler.Close()
	})

	router := gin.New()
	router.Any("/webhook/iris", gin.WrapH(handler))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/webhook/iris", http.NoBody)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(t, http.StatusMethodNotAllowed, res.Code)
}

func TestBuildBotRuntime_FailsFastWhenBotProvisionFails(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	runtime, err := buildBotRuntime(t.Context(), nil, logger, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "failed to create bot")
}
