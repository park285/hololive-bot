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

package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/park285/iris-client-go/webhook"
	"github.com/park285/shared-go/pkg/workerpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func TestBuildBotWebhookHandlerMalformedJSONDoesNotConsumeDedupSlot(t *testing.T) {
	const (
		token     = "test-token"
		messageID = "message-id-malformed-first"
	)

	t.Setenv("IRIS_WEBHOOK_TOKEN", token)

	valkeyClient, _ := sharedtestutil.NewTestValkeyClient(t)
	cacheClient := cachemocks.NewLenientClient()
	cacheClient.GetClientFunc = func() valkey.Client { return valkeyClient }

	appConfig := &config.Config{
		Iris: config.IrisConfig{WebhookToken: token},
		Webhook: config.WebhookConfig{
			WorkerCount:    1,
			QueueSize:      8,
			EnqueueTimeout: 100 * time.Millisecond,
			HandlerTimeout: time.Second,
			MaxBodyBytes:   1024,
			DedupTTL:       time.Minute,
			DedupTimeout:   500 * time.Millisecond,
		},
	}
	messageHandler := &recordingWebhookMessageHandler{messages: make(chan *webhook.Message, 1)}
	webhookPool := workerpool.NewQueued(workerpool.QueuedConfig{Workers: 1, QueueSize: 8})
	t.Cleanup(webhookPool.StopAndWait)

	handler, err := BuildBotWebhookHandler(
		appConfig,
		messageHandler,
		BotWebhookRuntimeDependencies{Cache: cacheClient},
		webhookPool,
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, handler.Close())
	})

	malformedRequest := newBotWebhookTestRequest(t.Context(), token, messageID, "{invalid-json")
	malformedResponse := httptest.NewRecorder()
	handler.ServeHTTP(malformedResponse, malformedRequest)
	assert.Equal(t, http.StatusBadRequest, malformedResponse.Code)

	validRequest := newBotWebhookTestRequest(
		t.Context(),
		token,
		messageID,
		`{"text":"hello","room":"room-1","sender":"tester","userId":"user-1"}`,
	)
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, validRequest)
	require.Equal(t, http.StatusOK, validResponse.Code)

	select {
	case msg := <-messageHandler.messages:
		require.NotNil(t, msg)
		assert.Equal(t, "hello", msg.Msg)
		assert.Equal(t, "room-1", msg.Room)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("message handler was not called after valid body reused the malformed request message ID")
	}
}

func TestBuildBotWebhookHandlerRequiresHMACWhenConfigured(t *testing.T) {
	const token = "test-token"

	t.Setenv("IRIS_WEBHOOK_TOKEN", token)

	valkeyClient, _ := sharedtestutil.NewTestValkeyClient(t)
	cacheClient := cachemocks.NewLenientClient()
	cacheClient.GetClientFunc = func() valkey.Client { return valkeyClient }

	appConfig := &config.Config{
		Iris: config.IrisConfig{WebhookToken: token},
		Webhook: config.WebhookConfig{
			WorkerCount:    1,
			QueueSize:      8,
			EnqueueTimeout: 100 * time.Millisecond,
			HandlerTimeout: time.Second,
			MaxBodyBytes:   1024,
			DedupTTL:       time.Minute,
			DedupTimeout:   500 * time.Millisecond,
			RequireHMAC:    true,
		},
	}
	messageHandler := &recordingWebhookMessageHandler{messages: make(chan *webhook.Message, 1)}
	webhookPool := workerpool.NewQueued(workerpool.QueuedConfig{Workers: 1, QueueSize: 8})
	t.Cleanup(webhookPool.StopAndWait)

	handler, err := BuildBotWebhookHandler(
		appConfig,
		messageHandler,
		BotWebhookRuntimeDependencies{Cache: cacheClient},
		webhookPool,
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, handler.Close())
	})

	request := newBotWebhookTestRequest(
		t.Context(),
		token,
		"message-id-require-hmac",
		`{"text":"hello","room":"room-1","sender":"tester","userId":"user-1"}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
}

type recordingWebhookMessageHandler struct {
	messages chan *webhook.Message
}

func (h *recordingWebhookMessageHandler) HandleMessage(_ context.Context, msg *webhook.Message) {
	select {
	case h.messages <- msg:
	default:
	}
}

func newBotWebhookTestRequest(ctx context.Context, token, messageID, body string) *http.Request {
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/webhook/iris", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(webhook.HeaderIrisToken, token)
	request.Header.Set(webhook.HeaderIrisMessageID, messageID)

	return request
}
