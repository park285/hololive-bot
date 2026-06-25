// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package applifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// GroupComponent describes one logical runtime plane hosted inside a larger
// process. Start must be non-blocking and should report asynchronous failures
// through errCh, matching the existing runtime Start contract.
type GroupComponent struct {
	Name     string
	Start    func(context.Context, chan<- error)
	Shutdown func(context.Context) error
}

// GroupRuntime coordinates several logical runtime planes in a single process.
// Components start in declaration order and stop in reverse order. Put shared
// dependency providers first and externally-facing listeners last when the
// listener should drain before its dependencies are released.
type GroupRuntime struct {
	Logger     *slog.Logger
	Components []GroupComponent
}

func NewGroupRuntime(logger *slog.Logger, components ...GroupComponent) *GroupRuntime {
	return &GroupRuntime{Logger: logger, Components: append([]GroupComponent(nil), components...)}
}

func (g *GroupRuntime) Start(ctx context.Context, errCh chan<- error) {
	if g == nil {
		return
	}
	for i := range g.Components {
		component := g.Components[i]
		if component.Start == nil {
			continue
		}
		name := componentName(i, component.Name)
		component.Start(ctx, errCh)
		logInfo(g.Logger, "runtime component started", slog.String("component", name))
	}
}

func (g *GroupRuntime) Shutdown(ctx context.Context) error {
	if g == nil {
		return nil
	}
	var errs []error
	for i := len(g.Components) - 1; i >= 0; i-- {
		component := g.Components[i]
		if component.Shutdown == nil {
			continue
		}
		name := componentName(i, component.Name)
		if err := component.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown %s: %w", name, err))
			logError(g.Logger, fmt.Sprintf("runtime component shutdown failed: %s", name), err)
			continue
		}
		logInfo(g.Logger, "runtime component stopped", slog.String("component", name))
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
