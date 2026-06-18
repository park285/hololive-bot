package testutil

import (
	"context"
	"encoding/json"
	"fmt"
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

	resp, err := getTestServer(t, srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type: got %q want %q", ct, "application/json")
	}
	if receivedMethod != http.MethodGet {
		t.Fatalf("method: got %q want %q", receivedMethod, http.MethodGet)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
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

	resp, err := getTestServer(t, srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer closeResponseBody(t, resp)

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", resp.StatusCode, http.StatusNoContent)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("expected empty body, got %q", string(raw))
	}
}

func getTestServer(t *testing.T, url string) (*http.Response, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("nil response")
	}
	return resp, nil
}

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp == nil || resp.Body == nil {
		t.Fatal("response body is nil")
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}
