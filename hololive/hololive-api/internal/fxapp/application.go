package fxapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/telemetry"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	runtimeapp "github.com/kapu/hololive-api/internal/app"
	apiconfig "github.com/kapu/hololive-api/internal/config"
)

const processLifecycleTimeout = 30 * time.Second

type buildVersion string

type telemetryResource interface {
	Shutdown(context.Context) error
}

type runtimeResource interface {
	Start(context.Context, chan<- error)
	Shutdown(context.Context) error
	CloseContext(context.Context) error
}

type (
	telemetryFactory func(context.Context, telemetry.Config) (telemetryResource, error)
	runtimeFactory   func(context.Context, *apiconfig.RuntimeConfig, *slog.Logger) (runtimeResource, error)
)

type applicationDependencies struct {
	newTelemetry telemetryFactory
	buildRuntime runtimeFactory
}

type applicationState struct {
	coordinator *lifecycleCoordinator
}

type applicationParams struct {
	config       *apiconfig.RuntimeConfig
	logger       *slog.Logger
	version      string
	dependencies applicationDependencies
	extraOptions []fx.Option
}

type Application struct {
	app         *fx.App
	resources   *resourceOwner
	coordinator *lifecycleCoordinator
}

func New(
	ctx context.Context,
	config *apiconfig.RuntimeConfig,
	logger *slog.Logger,
	version string,
) (*Application, error) {
	application, err := newApplication(ctx, applicationParams{
		config:       config,
		logger:       logger,
		version:      version,
		dependencies: productionDependencies(),
	})
	if err != nil {
		return nil, fmt.Errorf("new fx application: %w", err)
	}

	return application, nil
}

func newApplication(ctx context.Context, params applicationParams) (*Application, error) {
	if params.config == nil {
		return nil, errors.New("hololive-api config must not be nil")
	}

	if params.logger == nil {
		return nil, errors.New("logger must not be nil")
	}

	if params.dependencies.newTelemetry == nil || params.dependencies.buildRuntime == nil {
		return nil, errors.New("application dependencies must not be nil")
	}

	resources := newResourceOwner()
	state := &applicationState{}
	options := applicationOptions(ctx, params, resources, state)
	fxApplication := fx.New(options...)

	if err := fxApplication.Err(); err != nil {
		return nil, errors.Join(fmt.Errorf("initialize Fx application: %w", err), resources.Close(ctx))
	}

	if state.coordinator == nil {
		return nil, errors.Join(errors.New("initialize Fx application: lifecycle coordinator was not registered"), resources.Close(ctx))
	}

	return &Application{
		app:         fxApplication,
		resources:   resources,
		coordinator: state.coordinator,
	}, nil
}

func applicationOptions(
	ctx context.Context,
	params applicationParams,
	resources *resourceOwner,
	state *applicationState,
) []fx.Option {
	moduleOptions := make([]fx.Option, 0, 3+len(params.extraOptions))

	moduleOptions = append(moduleOptions,
		fx.Supply(
			buildVersion(params.version),
			params.config,
			params.logger,
			resources,
			state,
		),
		fx.Provide(
			func() context.Context { return ctx },
			telemetryConstructor(params.dependencies.newTelemetry),
			runtimeConstructor(params.dependencies.buildRuntime),
			newSupervisor,
			newLifecycleCoordinator,
		),
		fx.Invoke(registerProcessLifecycle),
	)

	moduleOptions = append(moduleOptions, params.extraOptions...)

	return []fx.Option{
		fx.StartTimeout(processLifecycleTimeout),
		fx.StopTimeout(processLifecycleTimeout),
		fx.WithLogger(newFXEventLogger),
		fx.Module("hololive-api", moduleOptions...),
	}
}

func productionDependencies() applicationDependencies {
	return applicationDependencies{
		newTelemetry: func(ctx context.Context, config telemetry.Config) (telemetryResource, error) {
			provider, err := telemetry.NewProvider(ctx, config)
			if err != nil {
				return nil, fmt.Errorf("initialize telemetry provider: %w", err)
			}

			return provider, nil
		},
		buildRuntime: func(
			ctx context.Context,
			config *apiconfig.RuntimeConfig,
			logger *slog.Logger,
		) (runtimeResource, error) {
			runtime, err := runtimeapp.BuildRuntime(ctx, config, logger)
			if err != nil {
				return nil, fmt.Errorf("build aggregate runtime: %w", err)
			}

			return runtime, nil
		},
	}
}

func telemetryConstructor(factory telemetryFactory) func(
	context.Context,
	buildVersion,
	*apiconfig.RuntimeConfig,
	*slog.Logger,
	*resourceOwner,
) (telemetryResource, error) {
	return func(
		ctx context.Context,
		version buildVersion,
		config *apiconfig.RuntimeConfig,
		logger *slog.Logger,
		resources *resourceOwner,
	) (telemetryResource, error) {
		provider, err := factory(ctx, hololiveAPITelemetryConfig(config, string(version)))
		if err != nil {
			return nil, fmt.Errorf("create telemetry resource: %w", err)
		}

		resources.Add(func(closeCtx context.Context) error {
			if err := provider.Shutdown(closeCtx); err != nil {
				logDiagnosticError(logger, "telemetry provider shutdown failed", err)

				return fmt.Errorf("close telemetry provider: %w", err)
			}

			return nil
		})

		return provider, nil
	}
}

func runtimeConstructor(factory runtimeFactory) func(
	context.Context,
	*apiconfig.RuntimeConfig,
	*slog.Logger,
	telemetryResource,
	*resourceOwner,
) (runtimeResource, error) {
	return func(
		ctx context.Context,
		config *apiconfig.RuntimeConfig,
		logger *slog.Logger,
		_ telemetryResource,
		resources *resourceOwner,
	) (runtimeResource, error) {
		runtime, err := factory(ctx, config, logger)
		if err != nil {
			return nil, fmt.Errorf("create aggregate runtime resource: %w", err)
		}

		resources.AddResumable(runtime.CloseContext)

		return runtime, nil
	}
}

func newFXEventLogger(logger *slog.Logger) fxevent.Logger {
	fxLogger := &fxevent.SlogLogger{Logger: logger}
	fxLogger.UseLogLevel(slog.LevelDebug)
	fxLogger.UseErrorLevel(slog.LevelError)

	return fxLogger
}

func hololiveAPITelemetryConfig(config *apiconfig.RuntimeConfig, version string) telemetry.Config {
	return telemetry.Config{
		Enabled:        config.Tracing.Enabled,
		ServiceName:    "hololive-api",
		ServiceVersion: version,
		Environment:    config.Bot.Environment,
		OTLPEndpoint:   config.Tracing.Endpoint,
		OTLPInsecure:   config.Tracing.Insecure,
		SampleRate:     config.Tracing.SampleRate,
	}
}
