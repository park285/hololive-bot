package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestBuildRuntime_FailFastOnNilInputs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("nil config", func(t *testing.T) {
		runtime, err := BuildRuntime(context.Background(), nil, logger)
		if err == nil {
			t.Fatal("BuildRuntime() expected error for nil config")
		}
		if err.Error() != "config must not be nil" {
			t.Fatalf("BuildRuntime() error = %q, want %q", err.Error(), "config must not be nil")
		}
		if runtime != nil {
			t.Fatal("BuildRuntime() expected nil runtime on error")
		}
	})

	t.Run("nil logger", func(t *testing.T) {
		runtime, err := BuildRuntime(context.Background(), &config.Config{}, nil)
		if err == nil {
			t.Fatal("BuildRuntime() expected error for nil logger")
		}
		if err.Error() != "logger must not be nil" {
			t.Fatalf("BuildRuntime() error = %q, want %q", err.Error(), "logger must not be nil")
		}
		if runtime != nil {
			t.Fatal("BuildRuntime() expected nil runtime on error")
		}
	})
}
