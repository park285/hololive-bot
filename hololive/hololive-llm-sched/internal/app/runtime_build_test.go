package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestBuildLLMSchedulerRuntime_FailFastOnNilInputs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("nil config", func(t *testing.T) {
		runtime, err := BuildLLMSchedulerRuntime(context.Background(), nil, logger)
		if err == nil {
			t.Fatal("BuildLLMSchedulerRuntime() expected error for nil config")
		}
		if err.Error() != "llm scheduler config must not be nil" {
			t.Fatalf("BuildLLMSchedulerRuntime() error = %q, want %q", err.Error(), "llm scheduler config must not be nil")
		}
		if runtime != nil {
			t.Fatal("BuildLLMSchedulerRuntime() expected nil runtime on error")
		}
	})

	t.Run("nil logger", func(t *testing.T) {
		runtime, err := BuildLLMSchedulerRuntime(context.Background(), &config.LLMSchedulerConfig{}, nil)
		if err == nil {
			t.Fatal("BuildLLMSchedulerRuntime() expected error for nil logger")
		}
		if err.Error() != "logger must not be nil" {
			t.Fatalf("BuildLLMSchedulerRuntime() error = %q, want %q", err.Error(), "logger must not be nil")
		}
		if runtime != nil {
			t.Fatal("BuildLLMSchedulerRuntime() expected nil runtime on error")
		}
	})
}
