package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestBuildAdminAPIRuntime_FailFastOnNilInputs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("nil config", func(t *testing.T) {
		runtime, err := BuildAdminAPIRuntime(context.Background(), nil, logger)
		if err == nil {
			t.Fatal("BuildAdminAPIRuntime() expected error for nil config")
		}
		if err.Error() != "admin api config must not be nil" {
			t.Fatalf("BuildAdminAPIRuntime() error = %q, want %q", err.Error(), "admin api config must not be nil")
		}
		if runtime != nil {
			t.Fatal("BuildAdminAPIRuntime() expected nil runtime on error")
		}
	})

	t.Run("nil logger", func(t *testing.T) {
		runtime, err := BuildAdminAPIRuntime(context.Background(), &config.AdminAPIConfig{}, nil)
		if err == nil {
			t.Fatal("BuildAdminAPIRuntime() expected error for nil logger")
		}
		if err.Error() != "logger must not be nil" {
			t.Fatalf("BuildAdminAPIRuntime() error = %q, want %q", err.Error(), "logger must not be nil")
		}
		if runtime != nil {
			t.Fatal("BuildAdminAPIRuntime() expected nil runtime on error")
		}
	})
}
