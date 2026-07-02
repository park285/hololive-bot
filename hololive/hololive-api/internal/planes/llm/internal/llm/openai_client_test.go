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
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	json "github.com/park285/shared-go/pkg/json"
	sharedllm "github.com/park285/shared-go/pkg/llm"
)

func TestNewClient_DefaultOptions(t *testing.T) {
	client := NewClient("https://example.com/v1", "test-key", "gpt-test", slog.New(slog.NewTextHandler(os.Stdout, nil)))

	if client.schemaName != "event_summary" {
		t.Errorf("default schemaName = %q, want %q", client.schemaName, "event_summary")
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
	logger.LogAttrs(context.Background(), slog.LevelError, "test", llmProviderErrorAttrs(wrappedErr)...)
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
	if err := stdjson.Unmarshal([]byte(raw), apiErr); err != nil {
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
	client := NewClient("https://example.com/v1", "key", "model", nil, WithSchemaName("custom_schema"))

	if client.schemaName != "custom_schema" {
		t.Errorf("schemaName = %q, want %q", client.schemaName, "custom_schema")
	}
}

func TestNewClient_WithSchemaName_Empty(t *testing.T) {
	client := NewClient("https://example.com/v1", "key", "model", nil, WithSchemaName(""))

	if client.schemaName != "event_summary" {
		t.Errorf("empty WithSchemaName should keep default, got %q", client.schemaName)
	}
}

func TestNewClient_WithTemperature_Positive(t *testing.T) {
	client := NewClient("https://example.com/v1", "key", "model", nil, WithTemperature(0.7))

	if client.temperature == nil {
		t.Fatal("temperature should be set for positive value")
	}
	if *client.temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", *client.temperature)
	}
}

func TestNewClient_WithTemperature_Zero(t *testing.T) {
	client := NewClient("https://example.com/v1", "key", "model", nil, WithTemperature(0))

	if client.temperature != nil {
		t.Errorf("WithTemperature(0) should not set temperature, got %v", *client.temperature)
	}
}

func TestNewClient_WithTemperature_Negative(t *testing.T) {
	client := NewClient("https://example.com/v1", "key", "model", nil, WithTemperature(-1))

	if client.temperature != nil {
		t.Errorf("WithTemperature(-1) should not set temperature, got %v", *client.temperature)
	}
}

func TestNewClient_MultipleOptions(t *testing.T) {
	client := NewClient("https://example.com/v1", "key", "model", nil,
		WithSchemaName("member_news_summary"),
		WithTemperature(0.3),
	)

	if client.schemaName != "member_news_summary" {
		t.Errorf("schemaName = %q, want %q", client.schemaName, "member_news_summary")
	}
	if client.temperature == nil || *client.temperature != 0.3 {
		t.Errorf("temperature should be 0.3")
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
			client := NewClient("https://example.com/v1", "key", "model", nil, tt.opt)
			if client.webSearch != tt.wantWeb {
				t.Fatalf("webSearch = %v, want %v", client.webSearch, tt.wantWeb)
			}
		})
	}
}

func TestNewClient_WithChatCompletions(t *testing.T) {
	client := NewClient("https://example.com/v1", "key", "model", nil, WithChatCompletions())

	if !client.chatCompletions {
		t.Fatal("chatCompletions should be enabled")
	}
	if client.webSearch {
		t.Fatal("chatCompletions mode should disable webSearch")
	}
}

func TestNewClient_WithReasoningEffort(t *testing.T) {
	client := NewClient("https://example.com/v1", "key", "model", nil, WithReasoningEffort("high"))
	if client.reasoningEffort != "high" {
		t.Fatalf("reasoningEffort = %q, want %q", client.reasoningEffort, "high")
	}
}

func TestNewClient_WithReasoningEffort_EmptyIgnored(t *testing.T) {
	client := NewClient("https://example.com/v1", "key", "model", nil,
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
	var _ Client = NewClient("https://example.com/v1", "key", "model", nil)
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
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		costTracker:     tracker,
	}
	schema := map[string]any{"type": "object"}

	got, err := client.GenerateJSON(context.Background(), "system", "user", schema)
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

func TestOpenAIClientGenerateJSON_SanitizesDiscoveredEventsOnlyAfterFallback(t *testing.T) {
	generator := &fakeJSONGenerator{
		resp: sharedllm.JSONResponse{
			Text:         `{"summary":"ok","discovered_events":[{"id":"a"}]}`,
			Model:        "gpt-test",
			FallbackUsed: true,
		},
	}
	client := &OpenAIClient{
		generator:  generator,
		model:      "gpt-test",
		schemaName: "event_summary",
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	got, err := client.GenerateJSON(context.Background(), "system", "user", map[string]any{"type": "object"})
	if err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("unmarshal sanitized json: %v", err)
	}
	events, ok := payload["discovered_events"].([]any)
	if !ok || len(events) != 0 {
		t.Fatalf("discovered_events = %#v, want empty array", payload["discovered_events"])
	}
}

type fakeJSONGenerator struct {
	called bool
	resp   sharedllm.JSONResponse
	err    error
}

func (f *fakeJSONGenerator) GenerateJSON(context.Context, sharedllm.JSONRequest) (sharedllm.JSONResponse, error) {
	f.called = true
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

func TestSuppressDiscoveredEvents_NoField(t *testing.T) {
	raw := `{"summary":"ok","items":[1,2,3]}`

	sanitized, err := suppressFallbackDiscoveredEvents(raw)
	if err != nil {
		t.Fatalf("suppressFallbackDiscoveredEvents() error = %v", err)
	}
	if sanitized != raw {
		t.Fatalf("suppressFallbackDiscoveredEvents() = %q, want original %q", sanitized, raw)
	}
}

func TestSuppressDiscoveredEvents_WithField(t *testing.T) {
	raw := `{"summary":"ok","discovered_events":[{"id":"a"}]}`

	sanitized, err := suppressFallbackDiscoveredEvents(raw)
	if err != nil {
		t.Fatalf("suppressFallbackDiscoveredEvents() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(sanitized), &payload); err != nil {
		t.Fatalf("unmarshal sanitized json: %v", err)
	}

	if payload["summary"] != "ok" {
		t.Fatalf("summary = %v, want %q", payload["summary"], "ok")
	}

	events, ok := payload["discovered_events"].([]any)
	if !ok {
		t.Fatalf("discovered_events type = %T, want []any", payload["discovered_events"])
	}
	if len(events) != 0 {
		t.Fatalf("discovered_events length = %d, want 0", len(events))
	}
}

func TestSuppressDiscoveredEvents_InvalidJSON(t *testing.T) {
	_, err := suppressFallbackDiscoveredEvents(`{"summary":`)
	if err == nil {
		t.Fatal("invalid json should return error")
	}
}

func TestApplyFallbackPostProcess_SkipsWhenNoFallback(t *testing.T) {
	client := &OpenAIClient{schemaName: "event_summary"}

	raw := `{"summary":"ok","discovered_events":[{"id":"a"}]}`
	got := client.applyFallbackPostProcess(raw, false)
	if got != raw {
		t.Fatalf("applyFallbackPostProcess() = %q, want original %q", got, raw)
	}
}

func TestApplyFallbackPostProcess_SanitizesEventSummary(t *testing.T) {
	client := &OpenAIClient{schemaName: "event_summary"}

	got := client.applyFallbackPostProcess(`{"summary":"ok","discovered_events":[{"id":"a"}]}`, true)

	var payload map[string]any
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("unmarshal sanitized json: %v", err)
	}
	if events, ok := payload["discovered_events"].([]any); !ok || len(events) != 0 {
		t.Fatalf("discovered_events = %#v, want empty array", payload["discovered_events"])
	}
}
