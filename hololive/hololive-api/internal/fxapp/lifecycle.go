package fxapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type lifecycleCoordinator struct {
	runtime    runtimeResource
	resources  *resourceOwner
	supervisor *supervisor
	logger     *slog.Logger
	drainLimit time.Duration

	mu            sync.Mutex
	runtimeCancel context.CancelFunc
	resultErr     error
}

func newLifecycleCoordinator(
	runtime runtimeResource,
	resources *resourceOwner,
	supervisor *supervisor,
	logger *slog.Logger,
) *lifecycleCoordinator {
	return &lifecycleCoordinator{
		runtime:    runtime,
		resources:  resources,
		supervisor: supervisor,
		logger:     logger,
		drainLimit: constants.AppTimeout.Shutdown,
	}
}

func registerProcessLifecycle(lifecycle fx.Lifecycle, coordinator *lifecycleCoordinator, state *applicationState) {
	state.coordinator = coordinator
	lifecycle.Append(fx.Hook{
		OnStart: coordinator.OnStart,
		OnStop:  coordinator.OnStop,
	})
}

func (c *lifecycleCoordinator) OnStart(ctx context.Context) error {
	if c == nil || c.runtime == nil {
		return errors.New("runtime lifecycle resource must not be nil")
	}

	runtimeCtx, runtimeCancel := context.WithCancel(context.WithoutCancel(ctx))
	errCh := make(chan error, 1)

	c.mu.Lock()

	c.runtimeCancel = runtimeCancel
	c.mu.Unlock()

	c.supervisor.Start(errCh)
	c.runtime.Start(runtimeCtx, errCh)

	return nil
}

func (c *lifecycleCoordinator) OnStop(ctx context.Context) error {
	if c == nil {
		return nil
	}

	c.mu.Lock()

	runtimeCancel := c.runtimeCancel
	c.mu.Unlock()

	if runtimeCancel != nil {
		runtimeCancel()
	}

	if c.logger != nil {
		c.logger.Info("hololive-api draining runtime planes")
	}

	drainCtx, drainCancel := context.WithTimeout(ctx, c.drainLimit)
	shutdownErr := c.runtime.Shutdown(drainCtx)

	drainCancel()

	c.supervisor.Stop()
	c.resources.Close(ctx)

	resultErr := errors.Join(c.supervisor.Err(), wrapShutdownError(shutdownErr))
	c.mu.Lock()

	c.resultErr = resultErr
	c.mu.Unlock()

	return resultErr
}

func (c *lifecycleCoordinator) Err() error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.resultErr
}

func wrapShutdownError(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("drain runtime planes: %w", err)
}
