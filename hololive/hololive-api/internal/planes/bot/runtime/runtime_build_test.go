package botruntime

import (
	"log/slog"
	"testing"
)

func TestBuildRuntime_FailFastOnNilInputs(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	t.Run("nil config", func(t *testing.T) {
		runtime, err := BuildRuntime(t.Context(), nil, nil, Dependencies{}, logger)
		if err == nil {
			t.Fatal("BuildRuntime() expected error for nil config")
		}

		if err.Error() != "normalize runtime build inputs: bootstrap: config must not be nil" {
			t.Fatalf("BuildRuntime() error = %q, want %q", err.Error(), "normalize runtime build inputs: bootstrap: config must not be nil")
		}

		if runtime != nil {
			t.Fatal("BuildRuntime() expected nil runtime on error")
		}
	})
}
