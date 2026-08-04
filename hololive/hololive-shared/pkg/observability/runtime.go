package observability

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	sharedlogging "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/telemetry"
)

type Runtime interface {
	Run() error
	Close()
}

type provider interface {
	Shutdown(context.Context) error
}

type providerFactory func(context.Context, *telemetry.Config) (provider, error)

type ManagedRuntime[T Runtime] struct {
	runtime  T
	provider provider
	logger   *slog.Logger
	close    sync.Once
}

func BuildRuntime[T Runtime](
	ctx context.Context,
	config *telemetry.Config,
	logger *slog.Logger,
	build func(context.Context) (T, error),
) (*ManagedRuntime[T], error) {
	return buildRuntime(ctx, config, logger, build, newProvider)
}

func buildRuntime[T Runtime](
	ctx context.Context,
	config *telemetry.Config,
	logger *slog.Logger,
	build func(context.Context) (T, error),
	newProvider providerFactory,
) (*ManagedRuntime[T], error) {
	traceProvider, err := newProvider(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("initialize telemetry provider: %w", err)
	}

	runtime, err := build(ctx)
	if err != nil {
		shutdownProvider(ctx, traceProvider, logger)
		return nil, err
	}

	return &ManagedRuntime[T]{
		runtime:  runtime,
		provider: traceProvider,
		logger:   logger,
	}, nil
}

func newProvider(ctx context.Context, config *telemetry.Config) (provider, error) {
	return telemetry.NewProvider(ctx, *config)
}

func (r *ManagedRuntime[T]) Run() error {
	if r == nil {
		return nil
	}
	return r.runtime.Run()
}

func (r *ManagedRuntime[T]) Close() {
	if r == nil {
		return
	}
	r.close.Do(func() {
		r.runtime.Close()
		shutdownProvider(context.Background(), r.provider, r.logger)
	})
}

func shutdownProvider(ctx context.Context, traceProvider provider, logger *slog.Logger) {
	if traceProvider == nil {
		return
	}
	if err := traceProvider.Shutdown(ctx); err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error(
			"telemetry provider shutdown failed",
			slog.String("error", sharedlogging.RedactDiagnostic(err.Error())),
		)
	}
}
