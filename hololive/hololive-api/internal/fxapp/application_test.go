package fxapp

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"github.com/park285/shared-go/v2/pkg/telemetry"
	"go.uber.org/fx"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestApplicationGraphValidates(t *testing.T) {
	params := successfulApplicationParams(nil)

	err := fx.ValidateApp(applicationOptions(t.Context(), params, newResourceOwner(), &applicationState{})...)
	if err != nil {
		t.Fatalf("ValidateApp() error = %v", err)
	}
}

func TestApplicationConstructionUsesThirtySecondLifecycleBudgets(t *testing.T) {
	application, err := newApplication(t.Context(), successfulApplicationParams(nil))
	if err != nil {
		t.Fatalf("newApplication() error = %v", err)
	}

	t.Cleanup(func() {
		application.SafetyClose(t.Context())
	})

	if application.StartTimeout() != processLifecycleTimeout {
		t.Fatalf("StartTimeout() = %v, want %v", application.StartTimeout(), processLifecycleTimeout)
	}

	if application.StopTimeout() != processLifecycleTimeout {
		t.Fatalf("StopTimeout() = %v, want %v", application.StopTimeout(), processLifecycleTimeout)
	}
}

func TestApplicationStartsAndStopsAggregateLifecycle(t *testing.T) {
	var cleanup []string

	params := successfulApplicationParams(&cleanup)
	runtime := &applicationLifecycleTestRuntime{}

	params.dependencies.buildRuntime = func(context.Context, *settings.HololiveAPIConfig, *slog.Logger) (runtimeResource, error) {
		return runtime, nil
	}

	application, err := newApplication(t.Context(), params)
	if err != nil {
		t.Fatalf("newApplication() error = %v", err)
	}

	if err := application.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := application.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if runtime.startCalls != 1 || runtime.shutdownCalls != 1 || runtime.closeCalls != 1 {
		t.Fatalf(
			"runtime calls = (start=%d,shutdown=%d,close=%d), want (1,1,1)",
			runtime.startCalls,
			runtime.shutdownCalls,
			runtime.closeCalls,
		)
	}

	if want := []string{"telemetry"}; !reflect.DeepEqual(cleanup, want) {
		t.Fatalf("tail cleanup = %v, want %v", cleanup, want)
	}
}

func TestApplicationTelemetryFailureStopsBeforeRuntimeBuild(t *testing.T) {
	telemetryErr := errors.New("telemetry unavailable")
	runtimeBuilt := false
	params := successfulApplicationParams(nil)

	params.dependencies.newTelemetry = func(context.Context, telemetry.Config) (telemetryResource, error) {
		return nil, telemetryErr
	}
	params.dependencies.buildRuntime = func(context.Context, *settings.HololiveAPIConfig, *slog.Logger) (runtimeResource, error) {
		runtimeBuilt = true

		return &applicationTestRuntime{}, nil
	}

	_, err := newApplication(t.Context(), params)

	if !errors.Is(err, telemetryErr) {
		t.Fatalf("newApplication() error = %v, want telemetry error", err)
	}

	if runtimeBuilt {
		t.Fatal("runtime was built after telemetry construction failed")
	}
}

func TestApplicationRuntimeFailureClosesTelemetry(t *testing.T) {
	runtimeErr := errors.New("runtime build failed")
	telemetrySpy := &applicationTestTelemetry{}
	params := successfulApplicationParams(nil)

	params.dependencies.newTelemetry = func(context.Context, telemetry.Config) (telemetryResource, error) {
		return telemetrySpy, nil
	}
	params.dependencies.buildRuntime = func(context.Context, *settings.HololiveAPIConfig, *slog.Logger) (runtimeResource, error) {
		return nil, runtimeErr
	}

	_, err := newApplication(t.Context(), params)

	if !errors.Is(err, runtimeErr) {
		t.Fatalf("newApplication() error = %v, want runtime error", err)
	}

	if telemetrySpy.shutdownCalls != 1 {
		t.Fatalf("telemetry Shutdown() calls = %d, want 1", telemetrySpy.shutdownCalls)
	}
}

func TestApplicationInvokeFailureClosesResourcesInReverseOrder(t *testing.T) {
	invokeErr := errors.New("invoke failed")

	var cleanup []string

	params := successfulApplicationParams(&cleanup)

	params.extraOptions = []fx.Option{
		fx.Invoke(func(runtimeResource) error {
			return invokeErr
		}),
	}

	_, err := newApplication(t.Context(), params)

	if !errors.Is(err, invokeErr) {
		t.Fatalf("newApplication() error = %v, want invoke error", err)
	}

	if want := []string{"runtime", "telemetry"}; !reflect.DeepEqual(cleanup, want) {
		t.Fatalf("cleanup order = %v, want %v", cleanup, want)
	}
}

func TestResourceOwnerClosesInReverseOrderExactlyOnce(t *testing.T) {
	owner := newResourceOwner()

	var calls []string

	owner.Add(func(context.Context) { calls = append(calls, "telemetry") })
	owner.Add(func(context.Context) { calls = append(calls, "runtime") })

	owner.Close(t.Context())
	owner.Close(t.Context())

	if want := []string{"runtime", "telemetry"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("Close() calls = %v, want %v", calls, want)
	}
}

func TestHololiveAPITelemetryConfigUsesFixedIdentity(t *testing.T) {
	config := &settings.HololiveAPIConfig{
		Bot: &settings.Config{Environment: "production"},
		Tracing: settings.TracingConfig{
			Enabled:    true,
			Endpoint:   "otel-collector:4317",
			Insecure:   true,
			SampleRate: 0.25,
		},
	}

	got := hololiveAPITelemetryConfig(config, "1.2.3")

	if got.ServiceName != "hololive-api" || got.ServiceVersion != "1.2.3" || got.Environment != "production" {
		t.Fatalf("telemetry identity = %#v, want fixed hololive-api identity", got)
	}

	if !got.Enabled || got.OTLPEndpoint != "otel-collector:4317" || !got.OTLPInsecure || got.SampleRate != 0.25 {
		t.Fatalf("telemetry config = %#v, want tracing settings preserved", got)
	}
}

type applicationTestTelemetry struct {
	shutdownCalls int
	cleanup       *[]string
}

func (r *applicationTestTelemetry) Shutdown(context.Context) error {
	r.shutdownCalls++
	if r.cleanup != nil {
		*r.cleanup = append(*r.cleanup, "telemetry")
	}

	return nil
}

type applicationTestRuntime struct {
	cleanup *[]string
}

type applicationLifecycleTestRuntime struct {
	startCalls    int
	shutdownCalls int
	closeCalls    int
}

func (r *applicationLifecycleTestRuntime) Start(context.Context, chan<- error) {
	r.startCalls++
}

func (r *applicationLifecycleTestRuntime) Shutdown(context.Context) error {
	r.shutdownCalls++

	return nil
}

func (r *applicationLifecycleTestRuntime) Close() {
	r.closeCalls++
}

func (r *applicationTestRuntime) Start(context.Context, chan<- error) {}

func (r *applicationTestRuntime) Shutdown(context.Context) error {
	return nil
}

func (r *applicationTestRuntime) Close() {
	if r.cleanup != nil {
		*r.cleanup = append(*r.cleanup, "runtime")
	}
}

func successfulApplicationParams(cleanup *[]string) applicationParams {
	logger := slog.New(slog.DiscardHandler)

	return applicationParams{
		config: &settings.HololiveAPIConfig{
			Bot: &settings.Config{Environment: "test"},
		},
		logger:  logger,
		version: "test",
		dependencies: applicationDependencies{
			newTelemetry: func(context.Context, telemetry.Config) (telemetryResource, error) {
				return &applicationTestTelemetry{cleanup: cleanup}, nil
			},
			buildRuntime: func(context.Context, *settings.HololiveAPIConfig, *slog.Logger) (runtimeResource, error) {
				return &applicationTestRuntime{cleanup: cleanup}, nil
			},
		},
	}
}
