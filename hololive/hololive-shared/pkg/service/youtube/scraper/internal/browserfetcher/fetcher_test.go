package browserfetcher

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type nilResponseTransport struct{}

func (nilResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestFetchPageNilResponse(t *testing.T) {
	fetcher := New("https://browser.example/snapshot", time.Second)
	fetcher.client.Transport = nilResponseTransport{}

	_, err := fetcher.FetchPage(t.Context(), Request{URL: "https://youtube.example/watch?v=test"})
	if err == nil {
		t.Fatal("expected error for nil HTTP response")
	}
	if got := err.Error(); !strings.Contains(got, "nil response") {
		t.Fatalf("error = %q, want nil response context", got)
	}
}
