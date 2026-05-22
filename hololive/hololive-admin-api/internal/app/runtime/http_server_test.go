package runtime

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartHTTPServerReportsListenErrorPrefix(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	logs := &countingLogHandler{}

	StartHTTPServer(&http.Server{Addr: "invalid::addr"}, slog.New(logs), errCh)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected HTTP server error, got nil")
		}
		if !strings.Contains(err.Error(), "HTTP server error") {
			t.Fatalf("error = %q, want HTTP server error prefix", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HTTP server error")
	}

	if got := logs.count.Load(); got != 0 {
		t.Fatalf("log count = %d, want 0 when errCh handles listen error", got)
	}
}

type countingLogHandler struct {
	count atomic.Int64
}

func (h *countingLogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *countingLogHandler) Handle(context.Context, slog.Record) error {
	h.count.Add(1)
	return nil
}

func (h *countingLogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *countingLogHandler) WithGroup(string) slog.Handler {
	return h
}
