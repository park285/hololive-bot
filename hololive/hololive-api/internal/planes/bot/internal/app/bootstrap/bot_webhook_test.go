package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v5/webhook"
	"github.com/park285/iris-client-go/v5/webhooksign"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestBuildDurableBotWebhookHandlerMalformedJSONDoesNotConsumeDedupSlot(t *testing.T) {
	const messageID = "message-id-malformed-first"

	admitter := &recordingWebhookAdmitter{messages: make(chan *webhook.Message, 1)}
	handler := buildDurableBotWebhookHandlerForTest(t, admitter)

	malformedRequest := newSignedBotWebhookTestRequest(t, messageID, "{invalid-json")
	malformedResponse := httptest.NewRecorder()
	handler.ServeHTTP(malformedResponse, malformedRequest)
	assert.Equal(t, http.StatusBadRequest, malformedResponse.Code)

	validRequest := newSignedBotWebhookTestRequest(
		t,
		messageID,
		`{"messageId":"message-id-malformed-first","text":"hello","room":"room-1","sender":"tester","userId":"user-1"}`,
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
	handler := buildDurableBotWebhookHandlerForTest(t, &recordingWebhookAdmitter{messages: make(chan *webhook.Message, 1)})

	request := newSignedBotWebhookTestRequest(t, "message-id-metrics", "{invalid-json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assertDefaultWebhookCounterAtLeast(t, "hololive_bot_webhook_bad_request_total", 1)
	assertDefaultWebhookCounterAtLeast(t, "hololive_bot_webhook_signature_v3_validated_total", 1)
}

func TestBuildDurableBotWebhookHandlerRequiresHMAC(t *testing.T) {
	const token = "test-token"

	handler := buildDurableBotWebhookHandlerForTest(t, &recordingWebhookAdmitter{messages: make(chan *webhook.Message, 1)})

	request := newBotWebhookTestRequest(
		t.Context(),
		token,
		"message-id-require-hmac",
		`{"messageId":"message-id-require-hmac","text":"hello","room":"room-1","sender":"tester","userId":"user-1"}`,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
}

func buildDurableBotWebhookHandlerForTest(t *testing.T, admitter webhook.MessageAdmitter) *webhook.Handler {
	t.Helper()

	return newDurableTestHandler(t, admitter, dbtest.NewPool(t))
}

func newDurableTestHandler(t *testing.T, admitter webhook.MessageAdmitter, pool *pgxpool.Pool) *webhook.Handler {
	t.Helper()

	handler, err := BuildDurableBotWebhookHandler(
		testDurableWebhookConfig(),
		admitter,
		pool,
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, handler.Close()) })

	return handler
}

func testDurableWebhookConfig() *apiconfig.BotPlaneConfig {
	return &apiconfig.BotPlaneConfig{
		Iris: settings.IrisConfig{WebhookToken: testWebhookToken},
		Webhook: apiconfig.WebhookConfig{
			MaxBodyBytes: 1024,
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

func (a *recordingWebhookAdmitter) AdmitMessage(_ context.Context, msg *webhook.Message, _ []byte) error {
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

const testWebhookToken = "test-token"

func newSignedBotWebhookTestRequest(t *testing.T, messageID, body string) *http.Request {
	t.Helper()

	request := newBotWebhookTestRequest(t.Context(), testWebhookToken, messageID, body)
	require.NoError(t, webhooksign.SignRequest(request, testWebhookToken, []byte(body)))

	return request
}
