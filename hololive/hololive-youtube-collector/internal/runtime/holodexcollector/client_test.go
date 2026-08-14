package holodexcollector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestClientRespectsRetryAfter(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.Client(), server.URL, "key", time.Second, 1024)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Fetch(context.Background())
	if collecterr.Code(err) != collecterr.Cooldown {
		t.Fatalf("error = %v", err)
	}
	retryAt, ok := collecterr.RetryAt(err)
	if !ok || retryAt.Before(time.Now().UTC()) {
		t.Fatalf("retry at = %s ok=%t", retryAt, ok)
	}
}
