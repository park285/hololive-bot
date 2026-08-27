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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustNewGeminiClient(t *testing.T, baseURL string, opts ...Option) *GeminiClient {
	t.Helper()

	client, err := NewGeminiClient(baseURL, "test-gemini-key", "gemini-3.7-flash", slog.New(slog.DiscardHandler), opts...)
	if err != nil {
		t.Fatalf("NewGeminiClient() error = %v", err)
	}

	return client
}

func writeGeminiTestResponse(t *testing.T, w io.Writer, response string) {
	t.Helper()

	if _, err := io.WriteString(w, response); err != nil {
		t.Errorf("write Gemini test response: %v", err)
	}
}

func writeGeminiTestResponsef(t *testing.T, w io.Writer, format string, args ...any) {
	t.Helper()

	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		t.Errorf("write Gemini test response: %v", err)
	}
}

func captureGeminiInteractionRequest(t *testing.T, r *http.Request) (geminiInteractionRequest, map[string]any) {
	t.Helper()

	assert.Equal(t, http.MethodPost, r.Method)
	assert.Equal(t, "/v1beta/interactions", r.URL.Path)
	assert.Equal(t, "test-gemini-key", r.Header.Get("x-goog-api-key"))

	var (
		captured   geminiInteractionRequest
		rawRequest map[string]any
	)

	raw, err := io.ReadAll(r.Body)
	if !assert.NoError(t, err) {
		return captured, rawRequest
	}

	assert.NoError(t, jsonv2.Unmarshal(raw, &captured))
	assert.NoError(t, jsonv2.Unmarshal(raw, &rawRequest))

	return captured, rawRequest
}

func assertGeminiGenerationConfig(t *testing.T, rawRequest map[string]any) {
	t.Helper()

	generationConfig, ok := rawRequest["generation_config"].(map[string]any)
	require.True(t, ok, "generation_config = %#v", rawRequest["generation_config"])

	for _, unsupported := range []string{"temperature", "top_p", "top_k"} {
		assert.NotContains(t, generationConfig, unsupported)
	}
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

	var (
		captured   geminiInteractionRequest
		rawRequest map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, rawRequest = captureGeminiInteractionRequest(t, r)

		w.Header().Set("Content-Type", "application/json")
		writeGeminiTestResponse(t, w, `{"status":"completed","steps":[{"type":"google_search_call","arguments":{"queries":["hololive"]}},{"type":"model_output","content":[{"type":"text","text":"{\"ok\":true}"}]}],"usage":{"total_tokens":321}}`)
	}))
	t.Cleanup(server.Close)

	client := mustNewGeminiClient(t, server.URL, WithReasoningEffort(testReasoningLevelHigh), WithWebSearch(true), WithCostTracker(tracker))

	got, err := client.GenerateJSON(t.Context(), "system contract", "user request", testObjectSchema())
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, got)
	assert.Equal(t, "gemini-3.7-flash", captured.Model)
	assert.Equal(t, "user request", captured.Input)
	assert.Equal(t, "system contract", captured.SystemInstruction)
	assert.Equal(t, testReasoningLevelHigh, captured.GenerationConfig.ThinkingLevel)
	assert.Equal(t, false, rawRequest["store"])
	require.Len(t, captured.Tools, 1)
	assert.Equal(t, "google_search", captured.Tools[0].Type)
	assert.Equal(t, "application/json", captured.ResponseFormat.MIMEType)
	assert.NotNil(t, captured.ResponseFormat.Schema)
	assertGeminiGenerationConfig(t, rawRequest)
	assert.Equal(t, []string{"gemini"}, tracker.providers)
	assert.Equal(t, []string{"gemini-3.7-flash"}, tracker.models)
	assert.Equal(t, []int64{321}, tracker.tokens)
}

func TestGeminiClientGenerateJSONOmitsSearchWhenDisabled(t *testing.T) {
	var captured geminiInteractionRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := jsonv2.UnmarshalRead(r.Body, &captured); err != nil {
			t.Errorf("decode request: %v", err)
		}

		writeGeminiTestResponse(t, w, `{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{}"}]}]}`)
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
		writeGeminiTestResponse(t, w, `{"status":"incomplete","steps":[{"type":"model_output","content":[{"type":"text","text":"{}"}]}]}`)
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
		writeGeminiTestResponsef(t, w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":%q}}`, secretMessage)
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
				writeGeminiTestResponse(t, w, response)
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
