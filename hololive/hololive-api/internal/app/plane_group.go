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

// planeComponent는 hololive-api 프로세스 안에 호스팅되는 하나의 논리적 runtime plane이다.
// Start는 non-blocking이어야 하며, 비동기 실패는 errCh로 보고해야 한다(기존 runtime
// Start 계약과 동일).
type planeComponent struct {
	Name     string
	Start    func(context.Context, chan<- error)
	Shutdown func(context.Context) error
}

// planeGroup은 여러 runtime plane을 한 프로세스에서 조율한다. 컴포넌트는 선언 순서로
// 시작하고 역순으로 정지한다. 어떤 listener가 의존성보다 먼저 drain돼야 하면 공유 의존성
// provider를 앞에, 외부 노출 listener를 뒤에 배치한다. 호출자는 nil이 아닌 logger를 넘긴다.
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
		// 한 component의 Shutdown panic이 뒤에 설 공유 의존성 provider의 정지를 건너뛰지 않도록 격리한다.
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
