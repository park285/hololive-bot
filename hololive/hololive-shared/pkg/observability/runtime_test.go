package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/telemetry"
)

type testRuntime struct {
	events  *[]string
	runErr  error
	closeOK bool
}

func (r *testRuntime) Run() error {
	*r.events = append(*r.events, "run")
	return r.runErr
}

func (r *testRuntime) Close() {
	*r.events = append(*r.events, "runtime_close")
	r.closeOK = true
}

type testProvider struct {
	events       *[]string
	shutdownErr  error
	shutdownCall int
}

func (p *testProvider) Shutdown(context.Context) error {
	*p.events = append(*p.events, "provider_shutdown")
	p.shutdownCall++
	return p.shutdownErr
}

func TestBuildRuntimeOwnsProviderUntilRuntimeClose(t *testing.T) {
	events := make([]string, 0, 3)
	traceProvider := &testProvider{events: &events}
	runtime := &testRuntime{events: &events}

	managed, err := buildRuntime(
		context.Background(),
		&telemetry.Config{Enabled: true, ServiceName: "test-service"},
		slog.Default(),
		func(context.Context) (*testRuntime, error) {
			return runtime, nil
		},
		func(context.Context, *telemetry.Config) (provider, error) {
			return traceProvider, nil
		},
	)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}

	if err := managed.Run(); err != nil {
		t.Fatalf("ManagedRuntime.Run() error = %v", err)
	}
	managed.Close()
	managed.Close()

	want := []string{"run", "runtime_close", "provider_shutdown"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}
	if !runtime.closeOK {
		t.Fatal("runtime was not closed")
	}
	if traceProvider.shutdownCall != 1 {
		t.Fatalf("provider Shutdown() calls = %d, want 1", traceProvider.shutdownCall)
	}
}

func TestBuildRuntimeShutsProviderDownWhenRuntimeBuildFails(t *testing.T) {
	events := make([]string, 0, 1)
	traceProvider := &testProvider{events: &events}
	wantErr := errors.New("runtime unavailable")

	managed, err := buildRuntime(
		context.Background(),
		&telemetry.Config{Enabled: true},
		slog.Default(),
		func(context.Context) (*testRuntime, error) {
			return nil, wantErr
		},
		func(context.Context, *telemetry.Config) (provider, error) {
			return traceProvider, nil
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("buildRuntime() error = %v, want %v", err, wantErr)
	}
	if managed != nil {
		t.Fatal("buildRuntime() returned a runtime after build failure")
	}
	if traceProvider.shutdownCall != 1 {
		t.Fatalf("provider Shutdown() calls = %d, want 1", traceProvider.shutdownCall)
	}
}

func TestManagedRuntimeShutsProviderDownAfterRuntimeRunFails(t *testing.T) {
	events := make([]string, 0, 3)
	wantErr := errors.New("runtime stopped")
	runtime := &testRuntime{events: &events, runErr: wantErr}
	traceProvider := &testProvider{events: &events}

	managed, err := buildRuntime(
		context.Background(),
		&telemetry.Config{Enabled: true},
		slog.Default(),
		func(context.Context) (*testRuntime, error) {
			return runtime, nil
		},
		func(context.Context, *telemetry.Config) (provider, error) {
			return traceProvider, nil
		},
	)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}

	if err := managed.Run(); !errors.Is(err, wantErr) {
		t.Fatalf("ManagedRuntime.Run() error = %v, want %v", err, wantErr)
	}
	managed.Close()

	want := []string{"run", "runtime_close", "provider_shutdown"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("lifecycle events = %v, want %v", events, want)
	}
	if traceProvider.shutdownCall != 1 {
		t.Fatalf("provider Shutdown() calls = %d, want 1", traceProvider.shutdownCall)
	}
}

func TestBuildRuntimeStopsBeforeRuntimeBuildWhenProviderFails(t *testing.T) {
	wantErr := errors.New("collector unavailable")
	buildCalled := false

	managed, err := buildRuntime(
		context.Background(),
		&telemetry.Config{Enabled: true},
		slog.Default(),
		func(context.Context) (*testRuntime, error) {
			buildCalled = true
			return nil, nil
		},
		func(context.Context, *telemetry.Config) (provider, error) {
			return nil, wantErr
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("buildRuntime() error = %v, want %v", err, wantErr)
	}
	if managed != nil {
		t.Fatal("buildRuntime() returned a runtime after provider failure")
	}
	if buildCalled {
		t.Fatal("runtime build started after provider failure")
	}
}

func TestManagedRuntimeRedactsProviderShutdownError(t *testing.T) {
	events := make([]string, 0, 2)
	runtime := &testRuntime{events: &events}
	traceProvider := &testProvider{
		events:      &events,
		shutdownErr: errors.New("export https://trace-user:trace-password@example.invalid failed"),
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	managed, err := buildRuntime(
		context.Background(),
		&telemetry.Config{Enabled: true},
		logger,
		func(context.Context) (*testRuntime, error) {
			return runtime, nil
		},
		func(context.Context, *telemetry.Config) (provider, error) {
			return traceProvider, nil
		},
	)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}

	managed.Close()

	if strings.Contains(output.String(), "trace-user") || strings.Contains(output.String(), "trace-password") {
		t.Fatalf("shutdown log contains credentials: %s", output.String())
	}
	if !strings.Contains(output.String(), "***REDACTED***@example.invalid") {
		t.Fatalf("shutdown log did not contain redacted diagnostic: %s", output.String())
	}
}
