package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
)

type testConfig struct {
	Logging sharedlogging.Config
	Port    int
}

type testRuntime struct {
	runCalls   int
	closeCalls int
}

func (r *testRuntime) Run() {
	r.runCalls++
}

func (r *testRuntime) Close() {
	r.closeCalls++
}

func TestRun_ReturnsExitCodeOneWhenLoadConfigFails(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("load boom")
	var stderr bytes.Buffer
	initCalled := false
	loggerCalled := false
	buildCalled := false

	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:                "test-version",
		Initialize:             func(string) { initCalled = true },
		LoadConfig:             func() (*testConfig, error) { return nil, loadErr },
		LoadConfigErrorMessage: "Failed to load test config",
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			loggerCalled = true
			return slog.New(slog.DiscardHandler), nil
		},
		BuildRuntime: func(context.Context, *testConfig, *slog.Logger) (*testRuntime, error) {
			buildCalled = true
			return &testRuntime{}, nil
		},
		Stderr: &stderr,
	})

	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if !initCalled {
		t.Fatal("Initialize() was not called")
	}
	if loggerCalled {
		t.Fatal("NewLogger() should not be called on config load failure")
	}
	if buildCalled {
		t.Fatal("BuildRuntime() should not be called on config load failure")
	}
	if got := stderr.String(); !strings.Contains(got, "Failed to load test config: load boom") {
		t.Fatalf("stderr = %q, want load failure message", got)
	}
}

func TestRun_ReturnsExitCodeOneWhenLoggerInitFails(t *testing.T) {
	t.Parallel()

	loggerErr := errors.New("logger boom")
	var stderr bytes.Buffer
	buildCalled := false

	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:                "test-version",
		Initialize:             func(string) {},
		LoadConfig:             func() (*testConfig, error) { return &testConfig{}, nil },
		LoadConfigErrorMessage: "Failed to load test config",
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			return nil, loggerErr
		},
		BuildRuntime: func(context.Context, *testConfig, *slog.Logger) (*testRuntime, error) {
			buildCalled = true
			return &testRuntime{}, nil
		},
		Stderr: &stderr,
	})

	if exitCode != 1 {
		t.Fatalf("Run() exitCode = %d, want 1", exitCode)
	}
	if buildCalled {
		t.Fatal("BuildRuntime() should not be called on logger init failure")
	}
	if got := stderr.String(); !strings.Contains(got, "Failed to initialize logger: logger boom") {
		t.Fatalf("stderr = %q, want logger init failure message", got)
	}
}

func TestRun_BuildsRunsAndClosesRuntime(t *testing.T) {
	t.Parallel()

	runtime := &testRuntime{}
	var initializedVersion string
	var builtConfig *testConfig
	var buildCtxDeadline time.Time
	buildStartedAt := time.Now()

	exitCode := Run(Options[*testConfig, *testRuntime]{
		Version:                "test-version",
		Initialize:             func(version string) { initializedVersion = version },
		LoadConfig:             func() (*testConfig, error) { return &testConfig{Port: 30001}, nil },
		LoadConfigErrorMessage: "Failed to load test config",
		NewLogger: func(*testConfig) (*slog.Logger, error) {
			return slog.New(slog.DiscardHandler), nil
		},
		StartupMessage: "Test runtime starting...",
		StartupFields: func(cfg *testConfig) []any {
			return []any{slog.Int("port", cfg.Port)}
		},
		BuildTimeout: 25 * time.Millisecond,
		BuildRuntime: func(ctx context.Context, cfg *testConfig, logger *slog.Logger) (*testRuntime, error) {
			if logger == nil {
				t.Fatal("BuildRuntime() logger = nil")
			}
			builtConfig = cfg
			var ok bool
			buildCtxDeadline, ok = ctx.Deadline()
			if !ok {
				t.Fatal("BuildRuntime() context missing deadline")
			}
			return runtime, nil
		},
		BuildErrorMessage: "Failed to build test runtime",
	})

	if exitCode != 0 {
		t.Fatalf("Run() exitCode = %d, want 0", exitCode)
	}
	if initializedVersion != "test-version" {
		t.Fatalf("Initialize() version = %q, want %q", initializedVersion, "test-version")
	}
	if builtConfig == nil || builtConfig.Port != 30001 {
		t.Fatalf("BuildRuntime() config = %#v, want port 30001", builtConfig)
	}
	buildTimeout := buildCtxDeadline.Sub(buildStartedAt)
	if buildTimeout <= 0 || buildTimeout > time.Second {
		t.Fatalf("BuildRuntime() timeout window = %v, want positive bounded timeout", buildTimeout)
	}
	if runtime.runCalls != 1 {
		t.Fatalf("runtime.Run() calls = %d, want 1", runtime.runCalls)
	}
	if runtime.closeCalls != 1 {
		t.Fatalf("runtime.Close() calls = %d, want 1", runtime.closeCalls)
	}
}
