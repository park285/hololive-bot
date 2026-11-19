package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/panicscope"
)

type planeComponent struct {
	Name     string
	Start    func(context.Context, chan<- error)
	Shutdown func(context.Context) error
}

type planeGroup struct {
	logger     *slog.Logger
	components []planeComponent
}

func newPlaneGroup(logger *slog.Logger, components ...planeComponent) *planeGroup {
	return &planeGroup{logger: logger, components: append([]planeComponent(nil), components...)}
}

func (g *planeGroup) Start(ctx context.Context, errCh chan<- error) {
	if g == nil {
		return
	}

	for i := range g.components {
		component := g.components[i]
		if component.Start == nil {
			continue
		}

		name := componentName(i, component.Name)
		component.Start(ctx, errCh)
		g.logger.Info("runtime component started", slog.String("component", name))
	}
}

func (g *planeGroup) Shutdown(ctx context.Context) error {
	if g == nil {
		return nil
	}

	var errs []error

	for i, component := range slices.Backward(g.components) {
		if component.Shutdown == nil {
			continue
		}

		name := componentName(i, component.Name)

		err := panicguard.RunE(g.logger, panicscope.BackgroundTask, name, func() error {
			return component.Shutdown(ctx)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("shutdown %s: %w", name, err))
			g.logger.Error(fmt.Sprintf("runtime component shutdown failed: %s", name), slog.Any("error", err))

			continue
		}

		g.logger.Info("runtime component stopped", slog.String("component", name))
	}

	return errors.Join(errs...)
}

func componentName(index int, name string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}

	return fmt.Sprintf("component[%d]", index)
}
