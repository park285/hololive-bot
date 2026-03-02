package llm

import (
	"context"
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
