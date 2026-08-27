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
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	sharedllm "github.com/park285/shared-go/v2/pkg/llm"
)

func mustNewClient(t *testing.T, baseURL, apiKey, model string, logger *slog.Logger, opts ...Option) *OpenAIClient {
	t.Helper()

	client, err := NewClient(baseURL, apiKey, model, logger, opts...)
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

	_, err := client.GenerateJSON(t.Context(), "system", "user", testObjectSchema())
	if err == nil {
		t.Fatal("GenerateJSON() error = nil, want Responses failure without Chat Completions fallback")
	}

	if strings.Join(paths, ",") != "/responses" {
		t.Fatalf("paths = %v, want /responses only", paths)
	}
}

func TestNewClient_EmptyAPIKeyReturnsError(t *testing.T) {
	client, err := NewClient("https://example.com/v1", "", "gpt-test", slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("NewClient() error = nil, want generator construction error")
	}

	if client != nil {
		t.Fatalf("NewClient() client = %#v, want nil", client)
	}
}

func TestNewClient_DefaultOptions(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "test-key", "gpt-test", slog.New(slog.NewTextHandler(os.Stdout, nil)))

	if client.schemaName != testDefaultSchemaName {
		t.Errorf("default schemaName = %q, want %q", client.schemaName, testDefaultSchemaName)
	}

	if client.temperature != nil {
		t.Errorf("default temperature = %v, want nil", *client.temperature)
	}

	if client.model != "gpt-test" {
		t.Errorf("model = %q, want %q", client.model, "gpt-test")
	}
}

func TestLLMProviderErrorAttrs_RedactsOpenAIRawJSON(t *testing.T) {
	apiErr := testOpenAIAPIError(t)
	wrappedErr := fmt.Errorf("provider failed: %w", apiErr)

	if !strings.Contains(wrappedErr.Error(), "private raw provider response") {
		t.Fatalf("test setup expected raw provider response in wrapped error, got: %s", wrappedErr.Error())
	}

	var buf bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.LogAttrs(t.Context(), slog.LevelError, "test", llmProviderErrorAttrs(wrappedErr)...)

	output := buf.String()

	if strings.Contains(output, "private raw provider response") {
		t.Fatalf("llmProviderErrorAttrs leaked raw provider response: %s", output)
	}

	if !strings.Contains(output, "status_code=429") {
		t.Fatalf("llmProviderErrorAttrs missing status_code, got: %s", output)
	}

	if !strings.Contains(output, "error_code=rate_limit") {
		t.Fatalf("llmProviderErrorAttrs missing error_code, got: %s", output)
	}

	if !strings.Contains(output, "provider_error_type=rate_limit_error") {
		t.Fatalf("llmProviderErrorAttrs missing provider error type, got: %s", output)
	}
}

func TestSafeLLMProviderError_RedactsOpenAIRawJSON(t *testing.T) {
	apiErr := testOpenAIAPIError(t)
	wrappedErr := fmt.Errorf("provider failed: %w", apiErr)

	safeErr := safeLLMProviderError(wrappedErr)
	if safeErr == nil {
		t.Fatal("safeLLMProviderError() = nil")
	}

	if strings.Contains(safeErr.Error(), "private raw provider response") {
		t.Fatalf("safeLLMProviderError leaked raw provider response: %s", safeErr.Error())
	}

	if !strings.Contains(safeErr.Error(), "status_code=429") {
		t.Fatalf("safeLLMProviderError missing status_code, got: %s", safeErr.Error())
	}

	if !strings.Contains(safeErr.Error(), "code=rate_limit") {
		t.Fatalf("safeLLMProviderError missing code, got: %s", safeErr.Error())
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

func testOpenAIAPIError(t *testing.T) *openai.Error {
	t.Helper()

	apiErr := &openai.Error{}
	raw := `{"code":"rate_limit","message":"private raw provider response","param":"messages","type":"rate_limit_error"}`

	if err := jsonv2.Unmarshal([]byte(raw), apiErr); err != nil {
		t.Fatalf("unmarshal openai error: %v", err)
	}

	apiErr.StatusCode = http.StatusTooManyRequests
	apiErr.Request = &http.Request{
		Method: http.MethodPost,
		URL: &url.URL{
			Scheme: "https",
			Host:   "api.openai.com",
			Path:   "/v1/responses",
		},
	}
	apiErr.Response = &http.Response{StatusCode: http.StatusTooManyRequests}

	return apiErr
}

func TestNewClient_WithSchemaName(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "key", "model", nil, WithSchemaName("custom_schema"))

	if client.schemaName != "custom_schema" {
		t.Errorf("schemaName = %q, want %q", client.schemaName, "custom_schema")
	}
}

func TestNewClient_WithSchemaName_Empty(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "key", "model", nil, WithSchemaName(""))

	if client.schemaName != testDefaultSchemaName {
		t.Errorf("empty WithSchemaName should keep default, got %q", client.schemaName)
	}
}

func TestNewClient_WithTemperature_Positive(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "key", "model", nil, WithTemperature(0.7))

	if client.temperature == nil {
		t.Fatal("temperature should be set for positive value")
	}

	if *client.temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", *client.temperature)
	}
}

func TestNewClient_WithTemperature_Zero(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "key", "model", nil, WithTemperature(0))

	if client.temperature != nil {
		t.Errorf("WithTemperature(0) should not set temperature, got %v", *client.temperature)
	}
}

func TestNewClient_WithTemperature_Negative(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "key", "model", nil, WithTemperature(-1))

	if client.temperature != nil {
		t.Errorf("WithTemperature(-1) should not set temperature, got %v", *client.temperature)
	}
}

func TestNewClient_MultipleOptions(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "key", "model", nil,
		WithSchemaName("member_news_summary"),
		WithTemperature(0.3),
	)

	if client.schemaName != "member_news_summary" {
		t.Errorf("schemaName = %q, want %q", client.schemaName, "member_news_summary")
	}

	if client.temperature == nil || *client.temperature != 0.3 {
		t.Error("temperature should be 0.3")
	}
}

func TestNewClient_WithWebSearch(t *testing.T) {
	tests := []struct {
		name    string
		opt     Option
		wantWeb bool
	}{
		{
			name:    "enable",
			opt:     WithWebSearch(true),
			wantWeb: true,
		},
		{
			name:    "disable",
			opt:     WithWebSearch(false),
			wantWeb: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mustNewClient(t, "https://example.com/v1", "key", "model", nil, tt.opt)
			if client.webSearch != tt.wantWeb {
				t.Fatalf("webSearch = %v, want %v", client.webSearch, tt.wantWeb)
			}
		})
	}
}

func TestNewClient_WithChatCompletions(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "key", "model", nil, WithChatCompletions())

	if !client.chatCompletions {
		t.Fatal("chatCompletions should be enabled")
	}

	if client.webSearch {
		t.Fatal("chatCompletions mode should disable webSearch")
	}
}

func TestNewClient_WithReasoningEffort(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "key", "model", nil, WithReasoningEffort("high"))
	if client.reasoningEffort != "high" {
		t.Fatalf("reasoningEffort = %q, want %q", client.reasoningEffort, "high")
	}
}

func TestNewClient_WithReasoningEffort_EmptyIgnored(t *testing.T) {
	client := mustNewClient(t, "https://example.com/v1", "key", "model", nil,
		WithReasoningEffort("high"),
		WithReasoningEffort(""),
	)
	if client.reasoningEffort != "high" {
		t.Fatalf("empty reasoning effort should be ignored, got %q", client.reasoningEffort)
	}
}

func TestOpenAIClient_ImplementsClient(t *testing.T) {
	// compile-time 검증 (var _ Client = (*OpenAIClient)(nil))은 openai_client.go에 존재
	// 런타임에서도 인터페이스 할당 가능 확인
	var _ Client = mustNewClient(t, "https://example.com/v1", "key", "model", nil)
}

func TestOpenAIClientGenerateJSON_DelegatesToSharedGenerator(t *testing.T) {
	temperature := 0.2
	generator := &fakeJSONGenerator{
		resp: sharedllm.JSONResponse{
			Text:  `{"ok":true}`,
			Model: "gpt-returned",
			Usage: sharedllm.Usage{TotalTokens: 9},
		},
	}
	tracker := &fakeCostTracker{}
	client := &OpenAIClient{
		generator:       generator,
		model:           "gpt-test",
		schemaName:      "member_news_summary",
		temperature:     &temperature,
		reasoningEffort: "high",
		webSearch:       false,
		chatCompletions: true,
		logger:          slog.New(slog.DiscardHandler),
		costTracker:     tracker,
	}
	schema := testObjectSchema()

	got, err := client.GenerateJSON(t.Context(), "system", "user", schema)
	if err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}

	if got != `{"ok":true}` {
		t.Fatalf("GenerateJSON() = %q, want JSON text", got)
	}

	if !generator.called {
		t.Fatal("shared generator was not called")
	}

	if len(tracker.tokens) == 0 || tracker.tokens[0] != 9 || tracker.models[0] != "gpt-returned" {
		t.Fatalf("usage tracker = models:%v tokens:%v", tracker.models, tracker.tokens)
	}
}

func TestOpenAIClientGenerateJSON_PreservesGemini37FlashHigh(t *testing.T) {
	generator := &fakeJSONGenerator{resp: sharedllm.JSONResponse{Text: `{"ok":true}`}}
	client := &OpenAIClient{
		generator:       generator,
		model:           "gemini-3.7-flash",
		schemaName:      "member_news_summary",
		reasoningEffort: "high",
		chatCompletions: true,
		logger:          slog.New(slog.DiscardHandler),
	}

	if _, err := client.GenerateJSON(t.Context(), "system", "user", testObjectSchema()); err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}

	if generator.req.Model != "gemini-3.7-flash" {
		t.Fatalf("request model = %q, want gemini-3.7-flash", generator.req.Model)
	}

	if generator.req.ReasoningEffort != "high" {
		t.Fatalf("request reasoning effort = %q, want high", generator.req.ReasoningEffort)
	}

	if !generator.req.ChatCompletions {
		t.Fatal("request must use Chat Completions")
	}

	if generator.req.Temperature != nil {
		t.Fatalf("request temperature = %v, want nil", *generator.req.Temperature)
	}
}

type fakeJSONGenerator struct {
	called bool
	req    sharedllm.JSONRequest
	resp   sharedllm.JSONResponse
	err    error
}

func (f *fakeJSONGenerator) GenerateJSON(_ context.Context, req sharedllm.JSONRequest) (sharedllm.JSONResponse, error) {
	f.called = true
	f.req = req
	return f.resp, f.err
}

func TestSafeLLMProviderError_RedactsEmptyOutputDiagnostics(t *testing.T) {
	rawErr := fmt.Errorf("%w: status=completed refusal=private raw provider response output=message/completed", errOpenAIEmptyOutput)

	safeErr := safeLLMProviderError(rawErr)
	if safeErr == nil {
		t.Fatal("safeLLMProviderError() = nil")
	}

	if strings.Contains(safeErr.Error(), "private raw provider response") {
		t.Fatalf("safeLLMProviderError leaked empty-output diagnostic: %s", safeErr.Error())
	}

	if !strings.Contains(safeErr.Error(), "error_type=openai_empty_output") {
		t.Fatalf("safeLLMProviderError missing empty-output type, got: %s", safeErr.Error())
	}
}
