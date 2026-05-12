package httputil

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestJSONClient_NewJSONRequestSetsHeadersAndBody(t *testing.T) {
	t.Parallel()

	client := NewJSONClient("https://example.com/", " secret-key ", 5*time.Second)

	req, err := client.NewJSONRequest(context.Background(), http.MethodPost, "/internal/test", map[string]any{
		"name": "kapu",
		"id":   7,
	})
	if err != nil {
		t.Fatalf("NewJSONRequest() error = %v", err)
	}

	if got, want := req.Method, http.MethodPost; got != want {
		t.Fatalf("req.Method = %s, want %s", got, want)
	}
	if got, want := req.URL.String(), "https://example.com/internal/test"; got != want {
		t.Fatalf("req.URL = %s, want %s", got, want)
	}
	if got, want := req.Header.Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got, want := req.Header.Get(apiKeyHeader), "secret-key"; got != want {
		t.Fatalf("API key header = %q, want %q", got, want)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}
	bodyText := string(body)
	if !strings.Contains(bodyText, `"name":"kapu"`) {
		t.Fatalf("body = %s, expected name field", bodyText)
	}
	if !strings.Contains(bodyText, `"id":7`) {
		t.Fatalf("body = %s, expected id field", bodyText)
	}
}

func TestJSONClient_NewRequestAppliesAPIKeyWithoutBody(t *testing.T) {
	t.Parallel()

	client := NewJSONClient("https://example.com", "token", 3*time.Second)

	req, err := client.NewRequest(context.Background(), http.MethodGet, "/health")
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	if got, want := req.URL.String(), "https://example.com/health"; got != want {
		t.Fatalf("req.URL = %s, want %s", got, want)
	}
	if got, want := req.Header.Get(apiKeyHeader), "token"; got != want {
		t.Fatalf("API key header = %q, want %q", got, want)
	}
}

func TestJSONClient_DiscardBodyClosesResponse(t *testing.T) {
	t.Parallel()

	client := NewJSONClient("https://example.com", "", time.Second)
	body := &trackCloseReadCloser{Reader: strings.NewReader(`{"ok":true}`)}

	if err := client.DiscardBody(&http.Response{Body: body}); err != nil {
		t.Fatalf("DiscardBody() error = %v", err)
	}
	if !body.closed {
		t.Fatal("DiscardBody() expected body close")
	}
}
