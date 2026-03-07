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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"testing"

	"github.com/openai/openai-go/v3"
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

func TestShouldFallbackToChat_NilError(t *testing.T) {
	if shouldFallbackToChat(nil) {
		t.Error("nil error should not trigger fallback")
	}
}

func TestShouldFallbackToChat_OpenAIStatusAndCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "responses_not_supported_404",
			err:  wrappedResponsesAPIError(http.StatusNotFound, ""),
			want: true,
		},
		{
			name: "responses_not_supported_405",
			err:  wrappedResponsesAPIError(http.StatusMethodNotAllowed, ""),
			want: true,
		},
		{
			name: "temporary_server_error_503",
			err:  wrappedResponsesAPIError(http.StatusServiceUnavailable, ""),
			want: true,
		},
		{
			name: "unsupported_code_400",
			err:  wrappedResponsesAPIError(http.StatusBadRequest, "unsupported_endpoint"),
			want: true,
		},
		{
			name: "invalid_request_400",
			err:  wrappedResponsesAPIError(http.StatusBadRequest, "invalid_request_error"),
			want: false,
		},
		{
			name: "unauthorized_401",
			err:  wrappedResponsesAPIError(http.StatusUnauthorized, "invalid_api_key"),
			want: false,
		},
		{
			name: "rate_limit_429",
			err:  wrappedResponsesAPIError(http.StatusTooManyRequests, "rate_limit_exceeded"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldFallbackToChat(tt.err); got != tt.want {
				t.Fatalf("shouldFallbackToChat() = %v, want %v (err=%v)", got, tt.want, tt.err)
			}
		})
	}
}

func TestShouldFallbackToChat_ContextCanceledOrDeadline_NoFallback(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "context_canceled",
			err:  fmt.Errorf("openai responses API: %w", context.Canceled),
		},
		{
			name: "context_deadline",
			err:  fmt.Errorf("openai responses API: %w", context.DeadlineExceeded),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if shouldFallbackToChat(tt.err) {
				t.Fatalf("%s should not fallback", tt.name)
			}
		})
	}
}

func TestShouldFallbackToChat_NetworkErrors(t *testing.T) {
	timeoutErr := fmt.Errorf("openai responses API: %w", &url.Error{
		Op:  "POST",
		URL: "https://example.com/v1/responses",
		Err: &net.DNSError{IsTimeout: true},
	})
	if !shouldFallbackToChat(timeoutErr) {
		t.Fatal("timeout network error should fallback")
	}

	connRefusedErr := fmt.Errorf("openai responses API: %w", &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: syscall.ECONNREFUSED,
	})
	if !shouldFallbackToChat(connRefusedErr) {
		t.Fatal("connection refused error should fallback")
	}

	nonRetryableNetErr := fmt.Errorf("openai responses API: %w", &url.Error{
		Op:  "POST",
		URL: "https://example.com/v1/responses",
		Err: errors.New("tls handshake failed"),
	})
	if shouldFallbackToChat(nonRetryableNetErr) {
		t.Fatal("non-timeout/non-conn-refused network error should not fallback")
	}
}

func TestSuppressDiscoveredEvents_NoField(t *testing.T) {
	raw := `{"summary":"ok","items":[1,2,3]}`

	sanitized, err := suppressDiscoveredEvents(raw)
	if err != nil {
		t.Fatalf("suppressDiscoveredEvents() error = %v", err)
	}
	if sanitized != raw {
		t.Fatalf("suppressDiscoveredEvents() = %q, want original %q", sanitized, raw)
	}
}

func TestSuppressDiscoveredEvents_WithField(t *testing.T) {
	raw := `{"summary":"ok","discovered_events":[{"id":"a"}]}`

	sanitized, err := suppressDiscoveredEvents(raw)
	if err != nil {
		t.Fatalf("suppressDiscoveredEvents() error = %v", err)
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
	_, err := suppressDiscoveredEvents(`{"summary":`)
	if err == nil {
		t.Fatal("invalid json should return error")
	}
}

func TestApplyFallbackPostProcess_SkipsWhenNoFallback(t *testing.T) {
	client := &OpenAIClient{schemaName: "event_summary"}

	raw := `{"summary":"ok","discovered_events":[{"id":"a"}]}`
	got, err := client.applyFallbackPostProcess(raw, false)
	if err != nil {
		t.Fatalf("applyFallbackPostProcess() error = %v", err)
	}
	if got != raw {
		t.Fatalf("applyFallbackPostProcess() = %q, want original %q", got, raw)
	}
}

func TestApplyFallbackPostProcess_SanitizesEventSummary(t *testing.T) {
	client := &OpenAIClient{schemaName: "event_summary"}

	got, err := client.applyFallbackPostProcess(`{"summary":"ok","discovered_events":[{"id":"a"}]}`, true)
	if err != nil {
		t.Fatalf("applyFallbackPostProcess() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("unmarshal sanitized json: %v", err)
	}
	if events, ok := payload["discovered_events"].([]any); !ok || len(events) != 0 {
		t.Fatalf("discovered_events = %#v, want empty array", payload["discovered_events"])
	}
}

func wrappedResponsesAPIError(statusCode int, code string) error {
	requestURL := &url.URL{
		Scheme: "https",
		Host:   "example.com",
		Path:   "/v1/responses",
	}

	apiErr := &openai.Error{
		Code:       code,
		StatusCode: statusCode,
		Request: &http.Request{
			Method: http.MethodPost,
			URL:    requestURL,
		},
		Response: &http.Response{
			StatusCode: statusCode,
			Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		},
	}
	return fmt.Errorf("openai responses API: %w", apiErr)
}
