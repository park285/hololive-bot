package chzzk

import (
	"net/http"
	"strings"
	"testing"
)

func TestNewRequestUsesRepositoryOwnedUserAgent(t *testing.T) {
	client := NewClient(nil, "", nil)
	req, err := client.newRequest(t.Context(), http.MethodGet, "https://chzzk.example/live")
	if err != nil {
		t.Fatalf("newRequest() error = %v", err)
	}

	got := req.Header.Get("User-Agent")
	if got != chzzkUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, chzzkUserAgent)
	}
	if strings.Contains(got, "capu.blog") {
		t.Fatalf("User-Agent = %q, deployment domain must not be hardcoded", got)
	}
}
