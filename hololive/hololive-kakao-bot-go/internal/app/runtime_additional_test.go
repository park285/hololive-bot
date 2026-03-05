package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBotRuntimeClose_CallsCleanup(t *testing.T) {
	t.Parallel()

	calls := 0
	runtime := &BotRuntime{
		cleanup: func() { calls++ },
	}

	runtime.Close()
	if calls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", calls)
	}

	var nilRuntime *BotRuntime
	nilRuntime.Close()
}

func TestBotRuntimeStartHTTPServer_Branches(t *testing.T) {
	t.Parallel()

	t.Run("nil runtime or nil server", func(t *testing.T) {
		var nilRuntime *BotRuntime
		nilRuntime.StartHTTPServer(make(chan error, 1))

		runtime := &BotRuntime{}
		runtime.StartHTTPServer(make(chan error, 1))
	})

	t.Run("listen error pushes err channel", func(t *testing.T) {
		runtime := &BotRuntime{
			HttpServer: &http.Server{Addr: "invalid::addr"},
		}
		errCh := make(chan error, 1)

		runtime.StartHTTPServer(errCh)

		select {
		case err := <-errCh:
			if err == nil || !strings.Contains(err.Error(), "HTTP server error") {
				t.Fatalf("unexpected error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for HTTP server error")
		}
	})
}

func TestBotRuntimeStartAndHelpers_NoPanicOnNilComponents(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	runtime := &BotRuntime{
		Logger:           logger,
		IngestionEnabled: false,
	}

	runtime.Start(context.Background(), nil)
	runtime.startBot(context.Background())
	runtime.logError("expected test error", errors.New("boom"))

	if !strings.Contains(logBuf.String(), "Ingestion runtime disabled on bot process") {
		t.Fatalf("log missing ingestion-disabled message: %s", logBuf.String())
	}
}

func TestBotRuntimeRun_ExitsOnServerError(t *testing.T) {
	t.Parallel()

	runtime := &BotRuntime{
		Logger:     slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		ServerAddr: "invalid::addr",
		HttpServer: &http.Server{
			Addr: "invalid::addr",
		},
		IngestionEnabled: false,
	}

	done := make(chan struct{})
	go func() {
		runtime.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not exit on server error")
	}
}

