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
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func mustNewGeminiClient(t *testing.T, baseURL string, opts ...Option) *GeminiClient {
	t.Helper()

	client, err := NewGeminiClient(baseURL, "test-gemini-key", "gemini-3.7-flash", slog.New(slog.DiscardHandler), opts...)
	if err != nil {
		t.Fatalf("NewGeminiClient() error = %v", err)
	}

	return client
}

func TestNewGeminiClientRejectsNonTLSRemoteBaseURL(t *testing.T) {
	client, err := NewGeminiClient("http://gemini.example", "test-key", "gemini-3.7-flash", slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("NewGeminiClient() error = %v, want HTTPS error", err)
	}

	if client != nil {
		t.Fatalf("NewGeminiClient() client = %#v, want nil", client)
	}
}

func TestSafeLLMProviderErrorPreservesGeminiMetadata(t *testing.T) {
	err := safeLLMProviderError(geminiProviderError{statusCode: http.StatusTooManyRequests, code: "RESOURCE_EXHAUSTED"})
	if err == nil {
		t.Fatal("safeLLMProviderError() error = nil")
	}

	for _, part := range []string{"status_code=429", "code=RESOURCE_EXHAUSTED", "api_type=gemini", "error_type=gemini_provider_error"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("safeLLMProviderError() = %q, want %q", err, part)
		}
	}
}

func TestGeminiClientGenerateJSONSendsNativeInteractionContract(t *testing.T) {
	tracker := &fakeCostTracker{}
	var captured geminiInteractionRequest
	var rawRequest map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1beta/interactions" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}

		if got := r.Header.Get("x-goog-api-key"); got != "test-gemini-key" {
			t.Errorf("x-goog-api-key = %q", got)
		}

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}

		if err := jsonv2.Unmarshal(raw, &captured); err != nil {
			t.Errorf("decode request: %v", err)
		}

		if err := jsonv2.Unmarshal(raw, &rawRequest); err != nil {
			t.Errorf("decode raw request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"completed","steps":[{"type":"google_search_call","arguments":{"queries":["hololive"]}},{"type":"model_output","content":[{"type":"text","text":"{\"ok\":true}"}]}],"usage":{"total_tokens":321}}`)
	}))
	t.Cleanup(server.Close)

	client := mustNewGeminiClient(t, server.URL, WithReasoningEffort("high"), WithWebSearch(true), WithCostTracker(tracker))
	got, err := client.GenerateJSON(t.Context(), "system contract", "user request", testObjectSchema())
	if err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}

	if got != `{"ok":true}` {
		t.Fatalf("GenerateJSON() = %q", got)
	}

	if captured.Model != "gemini-3.7-flash" || captured.Input != "user request" || captured.SystemInstruction != "system contract" {
		t.Fatalf("captured prompt contract = %#v", captured)
	}

	if captured.GenerationConfig.ThinkingLevel != "high" {
		t.Fatalf("thinking_level = %q", captured.GenerationConfig.ThinkingLevel)
	}

	if store, exists := rawRequest["store"]; !exists || store != false {
		t.Fatalf("store = %#v, want explicit false", store)
	}

	if len(captured.Tools) != 1 || captured.Tools[0].Type != "google_search" {
		t.Fatalf("tools = %#v", captured.Tools)
	}

	if captured.ResponseFormat.MIMEType != "application/json" || captured.ResponseFormat.Schema == nil {
		t.Fatalf("response_format = %#v", captured.ResponseFormat)
	}

	generationConfig, ok := rawRequest["generation_config"].(map[string]any)
	if !ok {
		t.Fatalf("generation_config = %#v", rawRequest["generation_config"])
	}

	for _, unsupported := range []string{"temperature", "top_p", "top_k"} {
		if _, exists := generationConfig[unsupported]; exists {
			t.Errorf("generation_config unexpectedly contains %q", unsupported)
		}
	}

	if !slices.Equal(tracker.providers, []string{"gemini"}) || !slices.Equal(tracker.models, []string{"gemini-3.7-flash"}) || !slices.Equal(tracker.tokens, []int64{321}) {
		t.Fatalf("tracked usage = providers %v models %v tokens %v", tracker.providers, tracker.models, tracker.tokens)
	}
}

func TestGeminiClientGenerateJSONOmitsSearchWhenDisabled(t *testing.T) {
	var captured geminiInteractionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := jsonv2.UnmarshalRead(r.Body, &captured); err != nil {
			t.Errorf("decode request: %v", err)
		}

		fmt.Fprint(w, `{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{}"}]}]}`)
	}))
	t.Cleanup(server.Close)

	client := mustNewGeminiClient(t, server.URL, WithWebSearch(false))
	if _, err := client.GenerateJSON(t.Context(), "system", "user", testObjectSchema()); err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}

	if len(captured.Tools) != 0 {
		t.Fatalf("tools = %#v, want omitted", captured.Tools)
	}
}

func TestGeminiClientGenerateJSONRejectsNonCompletedInteraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"incomplete","steps":[{"type":"model_output","content":[{"type":"text","text":"{}"}]}]}`)
	}))
	t.Cleanup(server.Close)

	client := mustNewGeminiClient(t, server.URL)
	_, err := client.GenerateJSON(t.Context(), "system", "user", testObjectSchema())
	if err == nil || !strings.Contains(err.Error(), "interaction_not_completed") {
		t.Fatalf("GenerateJSON() error = %v", err)
	}
}

func TestGeminiClientGenerateJSONRedactsHTTPErrorBody(t *testing.T) {
	const secretMessage = "private provider diagnostic"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintf(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":%q}}`, secretMessage)
	}))
	t.Cleanup(server.Close)

	client := mustNewGeminiClient(t, server.URL)
	_, err := client.GenerateJSON(t.Context(), "system", "user", testObjectSchema())
	if err == nil {
		t.Fatal("GenerateJSON() error = nil")
	}

	if strings.Contains(err.Error(), secretMessage) {
		t.Fatalf("GenerateJSON() leaked provider body: %v", err)
	}

	if !strings.Contains(err.Error(), "status_code=429") || !strings.Contains(err.Error(), "RESOURCE_EXHAUSTED") {
		t.Fatalf("GenerateJSON() error = %v", err)
	}
}

func TestGeminiClientGenerateJSONRejectsMalformedOrEmptyOutput(t *testing.T) {
	tests := map[string]string{
		"malformed response":  `{`,
		"empty output":        `{"status":"completed","steps":[{"type":"model_output","content":[]}]}`,
		"invalid JSON output": `{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"not-json"}]}]}`,
	}

	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, response)
			}))
			t.Cleanup(server.Close)

			client := mustNewGeminiClient(t, server.URL)
			if _, err := client.GenerateJSON(t.Context(), "system", "user", testObjectSchema()); err == nil {
				t.Fatal("GenerateJSON() error = nil")
			}
		})
	}
}

func TestGeminiClientGenerateJSONHonorsCanceledContext(t *testing.T) {
	client := mustNewGeminiClient(t, "https://generativelanguage.googleapis.com")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := client.GenerateJSON(ctx, "system", "user", testObjectSchema()); err == nil {
		t.Fatal("GenerateJSON() error = nil")
	}
}
