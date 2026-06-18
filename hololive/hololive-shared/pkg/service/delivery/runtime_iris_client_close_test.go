package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/park285/iris-client-go/iris"
)

func TestRuntimeIrisClientCloseRejectsSendAfterClose(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		writeRuntimeIrisResponse(t, w, `{"ok":true}`)
	}))
	defer server.Close()

	client := NewRuntimeIrisClient(server.URL, "bot-token", "", nil, iris.WithHTTPClient(server.Client()), iris.WithTransport("http1"))
	if err := client.SendMessage(context.Background(), "room-1", "hello"); err != nil {
		t.Fatalf("SendMessage() before Close error = %v, want nil", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}

	err := client.SendMessage(context.Background(), "room-1", "world")
	if err == nil || !strings.Contains(err.Error(), "client is closed") {
		t.Fatalf("SendMessage() after Close error = %v, want containing %q", err, "client is closed")
	}
}

func TestStaleCloseGraceExceedsReplyRetryBudget(t *testing.T) {
	t.Parallel()

	budget := runtimeIrisReplyAttemptTimeout * runtimeIrisReplyRetryMax
	if defaultStaleClientCloseGrace <= budget {
		t.Fatalf("defaultStaleClientCloseGrace=%s must exceed reply retry budget=%s (per-attempt %s × %d) so grace-close cannot sever an in-flight reply on base-URL rotation",
			defaultStaleClientCloseGrace, budget, runtimeIrisReplyAttemptTimeout, runtimeIrisReplyRetryMax)
	}
}
