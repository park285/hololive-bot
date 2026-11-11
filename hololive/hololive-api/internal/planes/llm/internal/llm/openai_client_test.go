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

package llm

import (
	"encoding/json/jsontext"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mustNewClient는 schema 이름을 기본값으로 먼저 넣는다. 뒤에 오는 WithSchemaName이 이를 덮어쓴다.
func mustNewClient(t *testing.T, baseURL, apiKey, model string, logger *slog.Logger, opts ...Option) *OpenAIClient {
	t.Helper()

	client, err := NewClient(baseURL, apiKey, model, logger, append([]Option{WithSchemaName(testDefaultSchemaName)}, opts...)...)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	return client
}

func TestNewClientDoesNotFallbackToChatCompletionsOnUnsupportedResponses(t *testing.T) {
	var paths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)

		http.Error(w, `{"error":{"message":"unsupported endpoint","type":"invalid_request_error","code":"unsupported_endpoint"}}`, http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	client := mustNewClient(t, server.URL, "test-key", "gpt-test", slog.New(slog.DiscardHandler), WithWebSearch(false))

	_, err := client.GenerateJSON(t.Context(), testPromptLayers(), testObjectSchema())
	if err == nil {
		t.Fatal("GenerateJSON() error = nil, want Responses failure without Chat Completions fallback")
	}

	if strings.Join(paths, ",") != "/responses" {
		t.Fatalf("paths = %v, want /responses only", paths)
	}
}

func TestNewClient_EmptyAPIKeyReturnsError(t *testing.T) {
	client, err := NewClient("https://example.com/v1", "", "gpt-test", slog.New(slog.DiscardHandler), WithSchemaName(testDefaultSchemaName))
	if err == nil {
		t.Fatal("NewClient() error = nil, want generator construction error")
	}

	if client != nil {
		t.Fatalf("NewClient() client = %#v, want nil", client)
	}
}

func TestNewClientRejectsEmptySchemaName(t *testing.T) {
	if _, err := NewClient("https://example.com/v1", "test-key", "gpt-test", slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("NewClient() error = nil, want empty schema name rejection")
	}
}

// provider 오류는 shared-go generator가 정제한 status_code·code를 유지하고 provider 원문은 옮기지 않는다.
func TestOpenAIClientErrorKeepsProviderStatusWithoutRawResponse(t *testing.T) {
	const rawResponse = "private raw provider response"

	server, _ := newCapturingLLMServer(t, http.StatusTooManyRequests,
		`{"error":{"message":"`+rawResponse+`","type":"rate_limit_error","code":"rate_limit","param":"messages"}}`)

	for _, tt := range []struct {
		name string
		opts []Option
	}{
		{name: "responses", opts: []Option{WithWebSearch(false)}},
		{name: "chat completions", opts: []Option{WithChatCompletions()}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := mustNewClient(t, server.URL, "test-key", "gpt-test", slog.New(slog.DiscardHandler), tt.opts...)

			_, err := client.GenerateJSON(t.Context(), testWirePromptLayers(), testObjectSchema())
			if err == nil {
				t.Fatal("GenerateJSON() error = nil, want 429 failure")
			}

			if strings.Contains(err.Error(), rawResponse) {
				t.Fatalf("GenerateJSON() error leaked raw provider response: %v", err)
			}

			for _, part := range []string{"status_code=429", "code=rate_limit"} {
				if !strings.Contains(err.Error(), part) {
					t.Fatalf("GenerateJSON() error = %v, want %q", err, part)
				}
			}
		})
	}
}

// provider 출력이 JSON 값 하나가 아니면 원문을 돌려주지 않고, 오류에도 출력 원문을 옮기지 않는다.
func TestOpenAIClientRejectsNonJSONOutputWithoutRawText(t *testing.T) {
	const rawOutput = "private raw provider output"

	server, _ := newCapturingLLMServer(t, http.StatusOK, `{"id":"resp_test","object":"response","created_at":0,"status":"completed","model":"gpt-test",`+
		`"output":[{"type":"message","id":"msg_test","status":"completed","role":"assistant",`+
		`"content":[{"type":"output_text","text":"`+rawOutput+`","annotations":[]}]}]}`)
	client := mustNewClient(t, server.URL, "test-key", "gpt-test", slog.New(slog.DiscardHandler), WithWebSearch(false))

	got, err := client.GenerateJSON(t.Context(), testWirePromptLayers(), testObjectSchema())
	if _, ok := errors.AsType[*jsontext.SyntacticError](err); !ok {
		t.Fatalf("GenerateJSON() = (%q, %v), want JSON syntax rejection", got, err)
	}

	if strings.Contains(err.Error(), rawOutput) {
		t.Fatalf("GenerateJSON() error leaked provider output: %v", err)
	}
}

func TestOpenAIClientRecordsProviderUsage(t *testing.T) {
	server, _ := newCapturingLLMServer(t, http.StatusOK, `{"id":"chatcmpl-test","object":"chat.completion","created":0,"model":"gpt-returned",`+
		`"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"ok\":true}"}}],`+
		`"usage":{"prompt_tokens":4,"completion_tokens":5,"total_tokens":9}}`)
	tracker := &fakeCostTracker{}
	client := mustNewClient(t, server.URL, "test-key", "gpt-test", slog.New(slog.DiscardHandler), WithChatCompletions(), WithCostTracker(tracker))

	if _, err := client.GenerateJSON(t.Context(), testWirePromptLayers(), testObjectSchema()); err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}

	if len(tracker.tokens) != 1 || tracker.tokens[0] != 9 || tracker.models[0] != "gpt-returned" || tracker.providers[0] != "openai" {
		t.Fatalf("usage tracker = providers:%v models:%v tokens:%v", tracker.providers, tracker.models, tracker.tokens)
	}
}

func TestOpenAIClientChatCompletionsPreservesGemini37FlashHigh(t *testing.T) {
	server, captured := newCapturingLLMServer(t, http.StatusBadRequest, badRequestBody)
	client := mustNewClient(t, server.URL, "test-key", "gemini-3.7-flash", slog.New(slog.DiscardHandler),
		WithSchemaName("member_news_summary"), WithChatCompletions(), WithReasoningEffort(testReasoningLevelHigh))

	if _, err := client.GenerateJSON(t.Context(), testPromptLayers(), testObjectSchema()); err == nil {
		t.Fatal("GenerateJSON() error = nil, want test server rejection")
	}

	path, body := captured.snapshot()
	if path != "/chat/completions" {
		t.Fatalf("path = %q, want /chat/completions", path)
	}

	if body["model"] != "gemini-3.7-flash" {
		t.Fatalf("request model = %v, want gemini-3.7-flash", body["model"])
	}

	if body["reasoning_effort"] != testReasoningLevelHigh {
		t.Fatalf("request reasoning effort = %v, want high", body["reasoning_effort"])
	}

	if temperature, ok := body["temperature"]; ok {
		t.Fatalf("request temperature = %v, want omitted", temperature)
	}
}

func TestSafeLLMProviderError_RedactsGenericProviderError(t *testing.T) {
	rawErr := errors.New("proxy leaked private raw provider response token=secret")

	safeErr := safeLLMProviderError(rawErr)
	if safeErr == nil {
		t.Fatal("safeLLMProviderError() = nil")
	}

	if strings.Contains(safeErr.Error(), "private raw provider response") {
		t.Fatalf("safeLLMProviderError leaked generic provider response: %s", safeErr.Error())
	}

	if strings.Contains(safeErr.Error(), "token=secret") {
		t.Fatalf("safeLLMProviderError leaked generic provider token: %s", safeErr.Error())
	}

	if !strings.Contains(safeErr.Error(), "error_type=errors.errorString") {
		t.Fatalf("safeLLMProviderError missing generic error type, got: %s", safeErr.Error())
	}
}

func TestOpenAIClient_ImplementsClient(t *testing.T) {
	var _ Client = mustNewClient(t, "https://example.com/v1", "key", "model", nil)
}
