package llm

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// LLM 호출은 비멱등 비용 작업이라 SDK가 재시도하지 않는다. 재시도 대상 상태(503)에도 요청은 한 번만 나가야 한다.
func TestLLMClientsDoNotRetryRetryableStatus(t *testing.T) {
	newUnavailableServer := func(t *testing.T) (*httptest.Server, *atomic.Int32) {
		t.Helper()

		var requests atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			http.Error(w, `{"error":{"message":"unavailable","type":"server_error"}}`, http.StatusServiceUnavailable)
		}))
		t.Cleanup(server.Close)

		return server, &requests
	}

	t.Run("openai compatible", func(t *testing.T) {
		server, requests := newUnavailableServer(t)
		client := mustNewClient(t, server.URL, "test-key", "gpt-test", slog.New(slog.DiscardHandler), WithWebSearch(false))

		if _, err := client.GenerateJSON(t.Context(), testPromptLayers(), testObjectSchema()); err == nil {
			t.Fatal("GenerateJSON() error = nil, want 503 failure")
		}

		if got := requests.Load(); got != 1 {
			t.Fatalf("requests = %d, want 1 (no SDK retry)", got)
		}
	})

	t.Run("preset", func(t *testing.T) {
		server, requests := newUnavailableServer(t)

		client, err := NewPresetClient(server.URL, "test-key", "gpt-test", slog.New(slog.DiscardHandler),
			WithSchemaName("member_news_summary"), WithChatCompletions(), WithWebSearch(false))
		if err != nil {
			t.Fatalf("NewPresetClient() error = %v", err)
		}

		if _, err := client.GenerateJSON(t.Context(), testWirePromptLayers(), testObjectSchema()); err == nil {
			t.Fatal("GenerateJSON() error = nil, want 503 failure")
		}

		if got := requests.Load(); got != 1 {
			t.Fatalf("requests = %d, want 1 (no SDK retry)", got)
		}
	})
}
