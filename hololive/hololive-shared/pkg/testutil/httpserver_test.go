package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestNewJSONTestServer(t *testing.T) {
	type payload struct {
		Message string `json:"message"`
	}

	body := payload{Message: "hello"}
	var receivedMethod string

	srv := NewJSONTestServer(t, http.StatusOK, body, func(r *http.Request) {
		receivedMethod = r.Method
	})

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type: got %q want %q", ct, "application/json")
	}
	if receivedMethod != http.MethodGet {
		t.Fatalf("method: got %q want %q", receivedMethod, http.MethodGet)
	}

	raw, _ := io.ReadAll(resp.Body)
	var got payload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Message != "hello" {
		t.Fatalf("message: got %q want %q", got.Message, "hello")
	}
}

func TestNewJSONTestServer_NilBody(t *testing.T) {
	srv := NewJSONTestServer(t, http.StatusNoContent, nil, nil)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}

	raw, _ := io.ReadAll(resp.Body)
	if len(raw) != 0 {
		t.Fatalf("expected empty body, got %q", string(raw))
	}
}
