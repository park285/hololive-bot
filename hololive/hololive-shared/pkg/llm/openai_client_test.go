package llm

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
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

func TestShouldFallbackToChat_ContextDeadline_NoFallback(t *testing.T) {
	err := fmt.Errorf("openai responses API: context deadline exceeded")
	if shouldFallbackToChat(err) {
		t.Error("context deadline exceeded should NOT trigger fallback (handled by ctx.Err() check)")
	}
}

func TestShouldFallbackToChat_NetworkTimeout_Fallback(t *testing.T) {
	err := fmt.Errorf("openai responses API: timeout")
	if !shouldFallbackToChat(err) {
		t.Error("network timeout should trigger fallback")
	}
}

func TestShouldFallbackToChat_ServerError_Fallback(t *testing.T) {
	err := fmt.Errorf("openai responses API: 502 bad gateway")
	if !shouldFallbackToChat(err) {
		t.Error("502 bad gateway should trigger fallback")
	}
}

func TestShouldFallbackToChat_NotFoundWithoutResponses_NoFallback(t *testing.T) {
	err := fmt.Errorf("openai chat completions API: 404 not found")
	if shouldFallbackToChat(err) {
		t.Error("404 without responses context should not trigger fallback")
	}
}

func TestShouldFallbackToChat_ResponsesNotFound_Fallback(t *testing.T) {
	err := fmt.Errorf("openai responses API: 404 not found")
	if !shouldFallbackToChat(err) {
		t.Error("responses 404 should trigger fallback")
	}
}

func TestShouldFallbackToChat_NilError(t *testing.T) {
	if shouldFallbackToChat(nil) {
		t.Error("nil error should not trigger fallback")
	}
}

func TestShouldFallbackToChat_UnknownError_NoFallback(t *testing.T) {
	err := fmt.Errorf("openai: rate limit exceeded")
	if shouldFallbackToChat(err) {
		t.Error("unknown error type should not trigger fallback")
	}
}
