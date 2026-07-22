package apiclient

import (
	"net/http"
	"strings"
	"testing"
)

func TestNewRequestUsesRepositoryOwnedUserAgent(t *testing.T) {
	client := &APIClient{}
	req, err := client.newRequest(t.Context(), http.MethodGet, "https://holodex.example/api/v2/live", "test-key")
	if err != nil {
		t.Fatalf("newRequest() error = %v", err)
	}

	got := req.Header.Get("User-Agent")
	if got != holodexUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, holodexUserAgent)
	}
	if strings.Contains(got, "capu.blog") {
		t.Fatalf("User-Agent = %q, deployment domain must not be hardcoded", got)
	}
}
