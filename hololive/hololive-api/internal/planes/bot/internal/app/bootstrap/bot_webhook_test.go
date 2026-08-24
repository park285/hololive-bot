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

	"github.com/park285/iris-client-go/v2/webhook"
	"github.com/park285/iris-client-go/v2/webhooksign"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestBuildDurableBotWebhookHandlerMalformedJSONDoesNotConsumeDedupSlot(t *testing.T) {
	const (
		token     = "test-token"
		messageID = "message-id-malformed-first"
	)

	t.Setenv("IRIS_WEBHOOK_TOKEN", "env-token-must-not-win")

	admitter := &recordingWebhookAdmitter{messages: make(chan *webhook.Message, 1)}
	handler := buildDurableBotWebhookHandlerForTest(t, admitter)

	malformedRequest := newSignedBotWebhookTestRequest(t, token, messageID, "{invalid-json")
	malformedResponse := httptest.NewRecorder()
	handler.ServeHTTP(malformedResponse, malformedRequest)
	assert.Equal(t, http.StatusBadRequest, malformedResponse.Code)

	validRequest := newSignedBotWebhookTestRequest(
		t,
		token,
		messageID,
		`{"text":"hello","room":"room-1","sender":"tester","userId":"user-1"}`,
	)
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, validRequest)
	require.Equal(t, http.StatusOK, validResponse.Code)

	select {
	case msg := <-admitter.messages:
		require.NotNil(t, msg)
		assert.Equal(t, "hello", msg.Msg)
		assert.Equal(t, "room-1", msg.Room)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("message admitter was not called after valid body reused the malformed request message ID")
	}
}

func TestBuildDurableBotWebhookHandlerWiresPrometheusMetrics(t *testing.T) {
	const token = "test-token"

	t.Setenv("IRIS_WEBHOOK_TOKEN", token)

	handler := buildDurableBotWebhookHandlerForTest(t, &recordingWebhookAdmitter{messages: make(chan *webhook.Message, 1)})

	request := newSignedBotWebhookTestRequest(t, token, "message-id-metrics", "{invalid-json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assertDefaultWebhookCounterAtLeast(t, "hololive_bot_webhook_bad_request_total", 1)
	assertDefaultWebhookCounterAtLeast(t, "hololive_bot_webhook_signature_v3_validated_total", 1)
}

func TestBuildDurableBotWebhookHandlerRequiresHMACWhenConfigured(t *testing.T) {
	const token = "test-token"

	t.Setenv("IRIS_WEBHOOK_TOKEN", token)

	valkeyClient, _ := sharedtestutil.NewTestValkeyClient(t)
	cacheClient := cachemocks.NewLenientClient()

	cacheClient.GetClientFunc = func() valkey.Client { return valkeyClient }

	appConfig := testDurableWebhookConfig()

	appConfig.Webhook.RequireHMAC = true

	handler, err := BuildDurableBotWebhookHandler(
		appConfig,
		&recordingWebhookAdmitter{messages: make(chan *webhook.Message, 1)},
		BotWebhookRuntimeDependencies{Cache: cacheClient},
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, handler.Close()) })

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

func buildDurableBotWebhookHandlerForTest(t *testing.T, admitter webhook.MessageAdmitter) *webhook.Handler {
	t.Helper()

	valkeyClient, _ := sharedtestutil.NewTestValkeyClient(t)
	cacheClient := cachemocks.NewLenientClient()

	cacheClient.GetClientFunc = func() valkey.Client { return valkeyClient }

	handler, err := BuildDurableBotWebhookHandler(
		testDurableWebhookConfig(),
		admitter,
		BotWebhookRuntimeDependencies{Cache: cacheClient},
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, handler.Close()) })

	return handler
}

func testDurableWebhookConfig() *settings.Config {
	return &settings.Config{
		Iris: settings.IrisConfig{WebhookToken: "test-token"},
		Webhook: settings.WebhookConfig{
			MaxBodyBytes: 1024,
			DedupTTL:     time.Minute,
			DedupTimeout: 500 * time.Millisecond,
		},
	}
}

func assertDefaultWebhookCounterAtLeast(t *testing.T, name string, minimum float64) {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() == name {
			require.NotEmpty(t, family.Metric)
			assert.GreaterOrEqual(t, family.Metric[0].GetCounter().GetValue(), minimum)

			return
		}
	}

	t.Fatalf("%s was not registered", name)
}

type recordingWebhookAdmitter struct {
	messages chan *webhook.Message
}

func (a *recordingWebhookAdmitter) AdmitMessage(_ context.Context, msg *webhook.Message) error {
	select {
	case a.messages <- msg:
	default:
	}

	return nil
}

func newBotWebhookTestRequest(ctx context.Context, token, messageID, body string) *http.Request {
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://hololive.example/webhook/iris", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Iris-Token", token)
	request.Header.Set(webhook.HeaderIrisMessageID, messageID)

	return request
}

func newSignedBotWebhookTestRequest(t *testing.T, token, messageID, body string) *http.Request {
	t.Helper()

	request := newBotWebhookTestRequest(t.Context(), token, messageID, body)
	require.NoError(t, webhooksign.SignRequest(request, token, []byte(body)))

	return request
}
